//go:build linux

package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
)

type subtitleTimelineHTTPProber struct{}

func (subtitleTimelineHTTPProber) CacheVersion() int             { return media.CurrentProbeVersion }
func (subtitleTimelineHTTPProber) ProbeFileJoinedContract() bool { return true }
func (subtitleTimelineHTTPProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	info, err := (introHTTPProber{}).ProbeFile(ctx, file)
	if err != nil {
		return info, err
	}
	info.Streams = []media.Stream{info.Streams[0], info.Streams[1],
		{Index: 0, CodecType: "subtitle", Codec: "hdmv_pgs_subtitle"},
		{Index: 9, CodecType: "subtitle", Codec: "dvd_subtitle"},
		{Index: 12, CodecType: "subtitle", Codec: "subrip", IsTextSubtitleStream: true}}
	return info, nil
}

func newSubtitleTimelineHTTPFixture(t *testing.T) *adminMetadataHTTPFixture {
	t.Helper()
	f := newAdminMetadataHTTPFixture(t)
	closeFixtureCatalogForReplacement(t, f.serverFixture)
	catalog, err := library.New(f.pool, subtitleTimelineHTTPProber{}, []string{f.root})
	if err != nil {
		t.Fatal(err)
	}
	installFixtureCatalog(t, f.serverFixture, catalog)
	f.handler = f.app.Handler()
	f.rescan(t, adminMetadataAutomaticNFO)
	return f
}

func subtitleTimelineHTTPCounts(t *testing.T, f *adminMetadataHTTPFixture) [6]int64 {
	t.Helper()
	var values [6]int64
	if err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM subtitle_timeline_queue),(SELECT count(*) FROM subtitle_timeline_requests),
		(SELECT count(*) FROM user_item_data),(SELECT count(*) FROM play_sessions),(SELECT count(*) FROM encoding_jobs),(SELECT count(*) FROM task_runs)`).Scan(&values[0], &values[1], &values[2], &values[3], &values[4], &values[5]); err != nil {
		t.Fatal(err)
	}
	return values
}

// Source identity, publication, integrity and authorization are real. The
// encoder supplies deterministic intervals rather than invoking a bitmap
// decoder; this fixture makes no decoder or OCR accuracy claim.
func publishSubtitleTimelineHTTPArtifact(t *testing.T, f *adminMetadataHTTPFixture, count int, force bool) library.SubtitleTimelineArtifact {
	t.Helper()
	actor, err := f.users.Resolve(f.ctx, f.cookie.Value, "admin")
	if err != nil {
		t.Fatal(err)
	}
	request := fmt.Sprintf("timeline-http-%d-%t", count, force)
	if _, err := f.app.library.QueueSubtitleTimelines(f.ctx, actor, library.AnalysisSelection{ItemIDs: []string{f.itemID}, Force: force}, request); err != nil {
		t.Fatal(err)
	}
	fence := library.AnalysisFence(func(library.OwnedTx) error { return nil })
	job, err := f.app.library.ClaimSubtitleTimeline(f.ctx, fence, f.libraryID, "timeline-http-run", "timeline-http-child")
	if err != nil || job == nil || job.SourceRevision == "" {
		t.Fatalf("claim timeline fixture: %+v %v", job, err)
	}
	artifact, err := f.app.library.GenerateSubtitleTimeline(f.ctx, *job, fence, func(_ context.Context, _ *os.File, source library.MediaFile, _ library.SubtitleTimelineJob, output io.Writer) (media.SubtitleTimelineSummary, error) {
		data := media.SubtitleTimelineData{Profile: media.SubtitleTimelineProfile, FFprobeSHA256: strings.Repeat("a", 64), DurationTicks: source.Item.Media.DurationTicks}
		for _, stream := range source.Item.Media.Streams {
			if stream.CodecType != "subtitle" || stream.Codec != "hdmv_pgs_subtitle" && stream.Codec != "dvd_subtitle" {
				continue
			}
			track := media.SubtitleTimelineTrack{SubtitleTimelineTrackSummary: media.SubtitleTimelineTrackSummary{StreamIndex: stream.Index, Codec: stream.Codec, IntervalCount: count}}
			for index := 0; index < count; index++ {
				track.Intervals = append(track.Intervals, media.SubtitleTimelineInterval{StartTicks: int64(index) * 100000, EndTicks: int64(index)*100000 + 50000})
			}
			data.Tracks = append(data.Tracks, track)
		}
		encoded, err := media.MarshalSubtitleTimelines(data)
		if err != nil {
			return media.SubtitleTimelineSummary{}, err
		}
		if _, err := output.Write(encoded); err != nil {
			return media.SubtitleTimelineSummary{}, err
		}
		return data.Summary(int64(len(encoded))), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.app.library.CompleteSubtitleTimeline(f.ctx, fence, *job, library.SubtitleTimelineResult{}); err != nil {
		t.Fatal(err)
	}
	return artifact
}

func subtitleTimelineHTTPDescriptor(t *testing.T, f *adminMetadataHTTPFixture, query string, headers http.Header) map[string]any {
	t.Helper()
	response := f.request(t, http.MethodGet, "/emby/Items/"+f.itemID+"/SubtitleTimelines"+query, nil, headers)
	expectStatus(t, response, http.StatusOK)
	if response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("timeline descriptor was publicly cacheable")
	}
	return jsonObject(t, response)
}

func TestHTTPSubtitleTimelinesExposeBoundedVersionedCoverageWithoutPlayback(t *testing.T) {
	f := newSubtitleTimelineHTTPFixture(t)
	headers := http.Header{"X-Emby-Token": {f.viewerToken}}
	base := "/emby/Items/" + f.itemID + "/SubtitleTimelines"
	before := subtitleTimelineHTTPCounts(t, f)
	missing := subtitleTimelineHTTPDescriptor(t, f, "", headers)
	if !reflect.DeepEqual(missing, map[string]any{"Available": false}) {
		t.Fatalf("missing timeline must be an empty fallback: %#v", missing)
	}
	expectStatus(t, f.request(t, http.MethodGet, base+"?Generate=true", nil, headers), http.StatusBadRequest)
	expectStatus(t, f.request(t, http.MethodGet, base+"/0?tag=missing", nil, headers), http.StatusNotFound)
	if subtitleTimelineHTTPCounts(t, f) != before {
		t.Fatal("missing timeline lookup created task or playback state")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(f.mediaPath), "backdrops", "goby-subtitle-timelines")); !os.IsNotExist(err) {
		t.Fatal("read-only descriptor created source-side storage")
	}
	artifact := publishSubtitleTimelineHTTPArtifact(t, f, 2, false)
	before = subtitleTimelineHTTPCounts(t, f)
	descriptor := subtitleTimelineHTTPDescriptor(t, f, "", headers)
	streams, ok := descriptor["Streams"].([]any)
	if descriptor["Available"] != true || descriptor["MediaSourceId"] != media.SourceID(f.itemID) || descriptor["SourceVersion"] != artifact.SourceStamp || !ok || len(streams) != 2 {
		t.Fatalf("bitmap-only descriptor: %#v", descriptor)
	}
	var oldURL string
	for index, codec := range []string{"hdmv_pgs_subtitle", "dvd_subtitle"} {
		stream := streams[index].(map[string]any)
		target := stringValue(t, stream, "Url")
		parsed, err := url.Parse(target)
		if err != nil || parsed.Query().Get("tag") != artifact.ETag || stream["Codec"] != codec || stream["IntervalCount"] != float64(2) {
			t.Fatalf("stream descriptor changed: %#v", stream)
		}
		response := f.request(t, http.MethodGet, target, nil, headers)
		expectStatus(t, response, http.StatusOK)
		payload := jsonObject(t, response)
		intervals, ok := payload["Intervals"].([]any)
		if len(payload) != 5 || payload["MediaSourceId"] != media.SourceID(f.itemID) || payload["SourceVersion"] != artifact.SourceStamp || payload["StreamIndex"] != stream["StreamIndex"] || payload["DurationTicks"] != float64(artifact.DurationTicks) || !ok || len(intervals) != 2 {
			t.Fatalf("track payload changed: %#v", payload)
		}
		if intervals[0].(map[string]any)["StartTicks"] != float64(0) || intervals[0].(map[string]any)["EndTicks"] != float64(50000) || intervals[1].(map[string]any)["StartTicks"] != float64(100000) {
			t.Fatal("zero start or the actual subtitle gap was lost")
		}
		for _, private := range []string{f.root, "FFprobeSHA256", "Warnings", "Generation", "SourceRevision", "Text", "Pixels"} {
			if strings.Contains(response.Body.String(), private) {
				t.Fatalf("consumer timeline exposed private/decoded content: %s", private)
			}
		}
		oldURL = target
	}
	for _, target := range []string{base + "?", base + "?UserId=a&UserId=b", oldURL + "&tag=other", oldURL + "&Unknown=1", base + "/00?tag=x", base + "/0"} {
		expectStatus(t, f.request(t, http.MethodGet, target, nil, headers), http.StatusBadRequest)
	}
	expectStatus(t, f.request(t, http.MethodGet, base+"/0?tag=obsolete", nil, headers), http.StatusNotFound)
	if subtitleTimelineHTTPCounts(t, f) != before {
		t.Fatal("timeline delivery queued work, encoded media or wrote viewing history")
	}
	updated := publishSubtitleTimelineHTTPArtifact(t, f, 10000, true)
	if updated.ETag == artifact.ETag {
		t.Fatal("explicit replacement reused the old generation tag")
	}
	before = subtitleTimelineHTTPCounts(t, f)
	expectStatus(t, f.request(t, http.MethodGet, oldURL, nil, headers), http.StatusNotFound)
	currentURL := base + "/0?tag=" + url.QueryEscape(updated.ETag)
	response := f.request(t, http.MethodGet, currentURL, nil, headers)
	expectStatus(t, response, http.StatusOK)
	if response.Body.Len() > 1<<20 || len(jsonObject(t, response)["Intervals"].([]any)) != 10000 {
		t.Fatal("maximum track exceeded its bounded JSON contract")
	}
	if err := os.WriteFile(f.mediaPath, []byte("changed indexed bitmap source"), 0600); err != nil {
		t.Fatal(err)
	}
	stale := subtitleTimelineHTTPDescriptor(t, f, "", headers)
	if stale["Available"] != false || stale["Streams"] != nil {
		t.Fatal("stale source exposed its prior subtitle timeline")
	}
	expectStatus(t, f.request(t, http.MethodGet, currentURL, nil, headers), http.StatusNotFound)
	key := sha256.Sum256([]byte(filepath.Base(f.mediaPath)))
	manifest := filepath.Join(filepath.Dir(f.mediaPath), "backdrops", "goby-subtitle-timelines", hex.EncodeToString(key[:]), "manifest.json")
	if err := os.WriteFile(manifest, []byte(`{"invalid":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if invalid := subtitleTimelineHTTPDescriptor(t, f, "", headers); invalid["Available"] != false || invalid["Streams"] != nil {
		t.Fatal("invalid optional material failed to hide its timeline")
	}
	if subtitleTimelineHTTPCounts(t, f) != before {
		t.Fatal("stale or invalid lookup regenerated material or wrote playback state")
	}
}

func TestHTTPSubtitleTimelinePermissionsKeepApplicationCatalogScope(t *testing.T) {
	f := newSubtitleTimelineHTTPFixture(t)
	headers := http.Header{"X-Emby-Token": {f.viewerToken}}
	base := "/emby/Items/" + f.itemID + "/SubtitleTimelines"
	expectStatus(t, f.request(t, http.MethodGet, base, nil, nil), http.StatusUnauthorized)
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Items/missing/SubtitleTimelines", nil, headers), http.StatusNotFound)
	writeAPIMediaFile(t, f.root, "timeline-shows/Example/Season 01/Example S01E01.mp4")
	television := createAndScanAPILibrary(t, f.serverFixture, f.cookie, f.csrf, filepath.Join(f.root, "timeline-shows"), "tvshows")
	var series string
	if err := f.pool.QueryRow(f.ctx, `SELECT id FROM items WHERE library_id=$1 AND type='Series' LIMIT 1`, television).Scan(&series); err != nil {
		t.Fatal(err)
	}
	folder := f.request(t, http.MethodGet, "/emby/Items/"+series+"/SubtitleTimelines", nil, headers)
	expectStatus(t, folder, http.StatusOK)
	if !reflect.DeepEqual(jsonObject(t, folder), map[string]any{"Available": false}) {
		t.Fatal("series folder exposed a physical subtitle timeline")
	}
	artifact := publishSubtitleTimelineHTTPArtifact(t, f, 2, false)
	track := base + "/0?tag=" + url.QueryEscape(artifact.ETag)
	before := subtitleTimelineHTTPCounts(t, f)
	setHTTPUserPolicy(t, f.serverFixture, f.viewerID, `{"EnableAllFolders":true,"EnableMediaPlayback":false}`)
	expectStatus(t, f.request(t, http.MethodGet, base, nil, headers), http.StatusForbidden)
	expectStatus(t, f.request(t, http.MethodGet, track, nil, headers), http.StatusForbidden)
	setHTTPUserPolicy(t, f.serverFixture, f.viewerID, `{"EnableAllFolders":false,"EnabledFolders":[],"EnableMediaPlayback":true}`)
	expectStatus(t, f.request(t, http.MethodGet, base, nil, headers), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, track, nil, headers), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, base+"?UserId="+f.adminID, nil, headers), http.StatusForbidden)
	keys := applicationMediaIssueKeys(t, f.serverFixture, f.adminToken)
	expectStatus(t, f.request(t, http.MethodGet, base+"?UserId="+f.viewerID, nil, keys[0].headers), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, track+"&UserId="+f.viewerID, nil, keys[0].headers), http.StatusNotFound)
	setHTTPUserPolicy(t, f.serverFixture, f.viewerID, `{"EnableAllFolders":true,"EnableMediaPlayback":false}`)
	projected := subtitleTimelineHTTPDescriptor(t, f, "?UserId="+f.viewerID, keys[0].headers)
	if projected["Available"] != true {
		t.Fatal("projection playback policy replaced independent application authority")
	}
	streams := projected["Streams"].([]any)
	target := streams[0].(map[string]any)["Url"].(string)
	parsed, err := url.Parse(target)
	if err != nil || parsed.Query().Get("UserId") != f.viewerID {
		t.Fatal("track URL dropped target catalog scope")
	}
	expectStatus(t, f.request(t, http.MethodGet, target, nil, keys[0].headers), http.StatusOK)
	if _, err := f.users.RevokeApplicationKey(f.ctx, keys[1].principal, keys[0].key.ID); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.request(t, http.MethodGet, track, nil, keys[0].headers), http.StatusUnauthorized)
	if subtitleTimelineHTTPCounts(t, f) != before {
		t.Fatal("authorization checks changed generation or playback state")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE items SET media=NULL WHERE id=$1`, f.itemID); err != nil {
		t.Fatal(err)
	}
	if fallback := subtitleTimelineHTTPDescriptor(t, f, "", http.Header{"X-Emby-Token": {f.adminToken}}); !reflect.DeepEqual(fallback, map[string]any{"Available": false}) {
		t.Fatal("unprobed source did not hide its optional timeline")
	}
}

func TestHTTPSubtitleTimelineAdminAdmissionRetainsReceiptAndForce(t *testing.T) {
	f := newSubtitleTimelineHTTPFixture(t)
	list := "/admin/v1/subtitle-timelines"
	detail := "/admin/v1/items/" + f.itemID + "/subtitle-timelines"
	for _, path := range []string{list, detail} {
		expectStatus(t, f.request(t, http.MethodGet, path, nil, nil), http.StatusUnauthorized)
		expectStatus(t, f.request(t, http.MethodGet, path, nil, http.Header{"X-Emby-Token": {f.adminToken}}), http.StatusUnauthorized)
		response := f.request(t, http.MethodGet, path, nil, nil, f.cookie)
		expectStatus(t, response, http.StatusOK)
		if path == detail && objectValue(t, jsonObject(t, response), "Artifact")["Available"] != false {
			t.Fatal("native inventory invented timeline material")
		}
	}
	response := f.request(t, http.MethodGet, list+"?LibraryId="+f.libraryID+"&State=missing&StartIndex=1&Limit=1", nil, nil, f.cookie)
	expectStatus(t, response, http.StatusOK)
	page := jsonObject(t, response)
	if page["TotalRecordCount"] != float64(3) || len(page["Items"].([]any)) != 1 || page["Items"].([]any)[0].(map[string]any)["ItemId"] != f.secondID {
		t.Fatalf("native timeline pagination lost library scope: %#v", page)
	}
	for _, query := range []string{"?", "?Unknown=1", "?Limit=0", "?Limit=1&Limit=2", "?SearchTerm=%00", "?SearchTerm=%ff"} {
		expectStatus(t, f.request(t, http.MethodGet, list+query, nil, nil, f.cookie), http.StatusBadRequest)
	}
	path := "/admin/v1/media-analysis/runs"
	body := map[string]any{"Kind": "subtitle-timeline", "RequestId": "timeline-admin-retry", "LibraryIds": []string{f.libraryID}, "ItemIds": []string{f.itemID}, "Force": true}
	headers := http.Header{"X-CSRF-Token": {f.csrf}}
	before := subtitleTimelineHTTPCounts(t, f)
	expectStatus(t, f.request(t, http.MethodPost, path, body, nil, f.cookie), http.StatusForbidden)
	expectStatus(t, f.request(t, http.MethodPost, path, body, http.Header{"X-CSRF-Token": {f.csrf}, "Origin": {"https://outside.example"}}, f.cookie), http.StatusForbidden)
	f.app.subtitleTimelines = nil
	expectStatus(t, f.request(t, http.MethodPost, path, body, headers, f.cookie), http.StatusServiceUnavailable)
	if subtitleTimelineHTTPCounts(t, f) != before {
		t.Fatal("unavailable admission created a durable timeline request")
	}
	// Admit against an explicit no-decoder executor to test HTTP receipts only.
	// The runtime flag enables this fixture's gate; real decoder availability
	// and PGS/DVD extraction remain separate acceptance tests.
	if err := f.app.taskManager.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	registry, err := tasks.NewExecutorRegistry(tasks.ExecutorRegistration{Key: library.TaskSubtitleTimelineGenerationKey, Name: "Timeline HTTP admission fixture", Executor: analysisHTTPNoWorkExecutor{}})
	if err != nil {
		t.Fatal(err)
	}
	f.app.taskStore, err = tasks.New(f.pool, f.app.library, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.app.taskStore.Reconcile(f.ctx); err != nil {
		t.Fatal(err)
	}
	f.app.taskManager, err = tasks.NewManager(f.app.taskStore, f.app.library, tasks.ManagerOptions{Logger: f.log})
	if err != nil {
		t.Fatal(err)
	}
	manager := f.app.taskManager
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	f.app.subtitleTimelines = &subtitleTimelineRuntime{available: true}
	response = f.request(t, http.MethodPost, path, body, headers, f.cookie)
	expectStatus(t, response, http.StatusAccepted)
	first := jsonObject(t, response)
	if len(first) != 4 || first["Queued"] != float64(1) || first["RunId"] == nil || first["TaskId"] == nil {
		t.Fatalf("timeline admission receipt changed: %#v", first)
	}
	response = f.request(t, http.MethodPost, path, body, headers, f.cookie)
	expectStatus(t, response, http.StatusAccepted)
	replayed := jsonObject(t, response)
	if replayed["RunId"] != first["RunId"] || replayed["TaskId"] != first["TaskId"] || replayed["Admitted"] != false || replayed["Queued"] != float64(1) {
		t.Fatal("HTTP retry admitted another timeline request")
	}
	var revision int64
	var forced bool
	var actor string
	if err := f.pool.QueryRow(f.ctx, `SELECT requested_revision,force,actor_user_id FROM subtitle_timeline_queue WHERE item_id=$1`, f.itemID).Scan(&revision, &forced, &actor); err != nil || revision != 1 || !forced || actor != f.adminID {
		t.Fatal("admission lost explicit Force or native actor", err)
	}
	body["Force"] = false
	expectAPIError(t, f.request(t, http.MethodPost, path, body, headers, f.cookie), http.StatusConflict, "subtitle_timeline_conflict", false)
	after := subtitleTimelineHTTPCounts(t, f)
	if after[0] != before[0]+1 || after[1] != before[1]+1 || after[2] != before[2] || after[3] != before[3] || after[4] != before[4] {
		t.Fatalf("admission/replay affected extra sources or playback: before=%v after=%v", before, after)
	}
}
