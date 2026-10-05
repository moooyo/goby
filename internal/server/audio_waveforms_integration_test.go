//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
)

type audioWaveformHTTPProber struct{}

func (audioWaveformHTTPProber) CacheVersion() int             { return media.CurrentProbeVersion }
func (audioWaveformHTTPProber) ProbeFileJoinedContract() bool { return true }
func (audioWaveformHTTPProber) ProbeFile(ctx context.Context, file *os.File) (media.Info, error) {
	info, err := (introHTTPProber{}).ProbeFile(ctx, file)
	for index := range info.Streams {
		if info.Streams[index].CodecType == "audio" {
			info.Streams[index].Index = 0
		}
	}
	return info, err
}

func newAudioWaveformHTTPFixture(t *testing.T) *adminMetadataHTTPFixture {
	t.Helper()
	f := newAdminMetadataHTTPFixture(t)
	closeFixtureCatalogForReplacement(t, f.serverFixture)
	catalog, err := library.New(f.pool, audioWaveformHTTPProber{}, []string{f.root})
	if err != nil {
		t.Fatal(err)
	}
	installFixtureCatalog(t, f.serverFixture, catalog)
	f.handler = f.app.Handler()
	f.rescan(t, adminMetadataAutomaticNFO)
	return f
}

func audioWaveformHTTPCounts(t *testing.T, f *adminMetadataHTTPFixture) [5]int64 {
	t.Helper()
	var result [5]int64
	if err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM audio_waveform_queue),(SELECT count(*) FROM audio_waveform_requests),
		(SELECT count(*) FROM user_item_data),(SELECT count(*) FROM play_sessions),(SELECT count(*) FROM encoding_jobs)`).Scan(&result[0], &result[1], &result[2], &result[3], &result[4]); err != nil {
		t.Fatal(err)
	}
	return result
}

// Publication and source authorization use the real library implementation.
// Only PCM extraction is replaced with deterministic valid envelopes; these
// HTTP tests do not claim to verify FFmpeg decoding or signal extraction.
func publishAudioWaveformHTTPArtifact(t *testing.T, f *adminMetadataHTTPFixture) library.AudioWaveformArtifact {
	t.Helper()
	actor, err := f.users.Resolve(f.ctx, f.cookie.Value, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.library.QueueAudioWaveforms(f.ctx, actor, library.AnalysisSelection{ItemIDs: []string{f.itemID}}, "waveform-http-fixture"); err != nil {
		t.Fatal(err)
	}
	fence := library.AnalysisFence(func(library.OwnedTx) error { return nil })
	job, err := f.app.library.ClaimAudioWaveform(f.ctx, fence, f.libraryID, "waveform-http-run", "waveform-http-child")
	if err != nil || job == nil || job.SourceRevision == "" {
		t.Fatalf("claim waveform fixture: %+v %v", job, err)
	}
	artifact, err := f.app.library.GenerateAudioWaveform(f.ctx, *job, fence, func(_ context.Context, _ *os.File, source library.MediaFile, _ library.AudioWaveformJob, output io.Writer) (media.AudioWaveformSummary, error) {
		info := source.Item.Media
		data := media.AudioWaveformData{Profile: media.AudioWaveformProfile, FFmpegSHA256: strings.Repeat("a", 64), DurationTicks: info.DurationTicks}
		for _, stream := range info.Streams {
			if stream.CodecType != "audio" || stream.IsExternal || stream.IsAttachedPicture {
				continue
			}
			track := media.AudioWaveformTrack{AudioWaveformTrackSummary: media.AudioWaveformTrackSummary{StreamIndex: stream.Index,
				Channels: stream.Channels, SampleRate: stream.SampleRate, ChannelLayout: stream.ChannelLayout,
				SampleCount: info.DurationTicks * int64(stream.SampleRate) / media.TicksPerSecond, CoverageEndTicks: info.DurationTicks}}
			for _, count := range []int{512, 1024, 2048, 4096} {
				level := media.AudioWaveformLevel{BucketCount: count, Peaks: make([]uint16, count), RMS: make([]uint16, count), Validity: make([]byte, count/8)}
				for bucket := range count {
					level.Peaks[bucket], level.RMS[bucket] = 1000, 500
					level.Validity[bucket/8] |= 1 << uint(bucket%8)
				}
				track.Levels = append(track.Levels, level)
			}
			data.Tracks = append(data.Tracks, track)
		}
		encoded, err := media.MarshalAudioWaveforms(data)
		if err != nil {
			return media.AudioWaveformSummary{}, err
		}
		if _, err := output.Write(encoded); err != nil {
			return media.AudioWaveformSummary{}, err
		}
		return data.Summary(int64(len(encoded))), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := f.app.library.CompleteAudioWaveform(f.ctx, fence, *job, library.AudioWaveformResult{}); err != nil {
		t.Fatal(err)
	}
	return artifact
}

func audioWaveformHTTPDescriptor(t *testing.T, f *adminMetadataHTTPFixture, suffix string, headers http.Header) map[string]any {
	t.Helper()
	response := f.request(t, http.MethodGet, "/emby/Items/"+f.itemID+"/AudioWaveforms"+suffix, nil, headers)
	expectStatus(t, response, http.StatusOK)
	if response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatal("waveform descriptor was cacheable")
	}
	return jsonObject(t, response)
}

func TestHTTPAudioWaveformLevelsAreBoundedVersionedAndReadOnly(t *testing.T) {
	f := newAudioWaveformHTTPFixture(t)
	headers := http.Header{"X-Emby-Token": {f.viewerToken}}
	base := "/emby/Items/" + f.itemID + "/AudioWaveforms"
	before := audioWaveformHTTPCounts(t, f)
	if !reflect.DeepEqual(audioWaveformHTTPDescriptor(t, f, "", headers), map[string]any{"Available": false}) {
		t.Fatal("missing waveform did not fall back")
	}
	expectStatus(t, f.request(t, http.MethodGet, base+"/0?buckets=512&tag=missing", nil, headers), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, base+"?Generate=true", nil, headers), http.StatusBadRequest)
	if audioWaveformHTTPCounts(t, f) != before {
		t.Fatal("read-only lookup queued generation")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(f.mediaPath), "backdrops", "goby-waveforms")); !os.IsNotExist(err) {
		t.Fatal("lookup created waveform storage")
	}
	artifact := publishAudioWaveformHTTPArtifact(t, f)
	before = audioWaveformHTTPCounts(t, f)
	descriptor := audioWaveformHTTPDescriptor(t, f, "", headers)
	streams, ok := descriptor["Streams"].([]any)
	if descriptor["Available"] != true || descriptor["MediaSourceId"] != media.SourceID(f.itemID) || descriptor["SourceVersion"] != artifact.SourceStamp || !ok || len(streams) != 1 {
		t.Fatalf("descriptor: %#v", descriptor)
	}
	stream := streams[0].(map[string]any)
	levels, ok := stream["Levels"].([]any)
	if stream["StreamIndex"] != float64(0) || !ok || len(levels) != 4 {
		t.Fatal("stream zero or fixed LOD inventory was lost")
	}
	var finestURL string
	for index, count := range []int{512, 1024, 2048, 4096} {
		level := levels[index].(map[string]any)
		target := stringValue(t, level, "Url")
		parsed, err := url.Parse(target)
		if err != nil || parsed.Path != base+"/0" || parsed.Query().Get("tag") != artifact.ETag || level["BucketCount"] != float64(count) {
			t.Fatalf("level descriptor: %#v", level)
		}
		response := f.request(t, http.MethodGet, target, nil, headers)
		expectStatus(t, response, http.StatusOK)
		wire := response.Body.Bytes()
		if len(wire) != 32+count*4+count/8 || string(wire[:4]) != "GAWL" || binary.LittleEndian.Uint32(wire[8:]) != 0 || binary.LittleEndian.Uint32(wire[12:]) != uint32(count) ||
			binary.LittleEndian.Uint64(wire[16:]) != uint64(artifact.DurationTicks) || binary.LittleEndian.Uint16(wire[32:]) != 1000 || binary.LittleEndian.Uint16(wire[34:]) != 500 || wire[32+count*4] != 255 {
			t.Fatal("HTTP level did not preserve the validated peak/RMS envelope")
		}
		if response.Header().Get("Content-Type") != "application/octet-stream" || response.Header().Get("X-Content-Type-Options") != "nosniff" || response.Header().Get("Cache-Control") != "private, no-cache" || response.Header().Get("ETag") == "" {
			t.Fatal("level transport headers changed")
		}
		head := f.request(t, http.MethodHead, target, nil, headers)
		expectStatus(t, head, http.StatusOK)
		if head.Body.Len() != 0 || head.Header().Get("Content-Length") != strconv.Itoa(len(wire)) {
			t.Fatal("HEAD did not describe the level without a body")
		}
		rangeHeaders := headers.Clone()
		rangeHeaders.Set("Range", "bytes=0-31")
		ranged := f.request(t, http.MethodGet, target, nil, rangeHeaders)
		expectStatus(t, ranged, http.StatusPartialContent)
		if !bytes.Equal(ranged.Body.Bytes(), wire[:32]) {
			t.Fatal("level range did not return the exact GAWL header")
		}
		conditional := headers.Clone()
		conditional.Set("If-None-Match", response.Header().Get("ETag"))
		expectStatus(t, f.request(t, http.MethodGet, target, nil, conditional), http.StatusNotModified)
		finestURL = target
	}
	for _, target := range []string{finestURL + "&tag=other", finestURL + "&buckets=512", finestURL + "&Unknown=true", base + "/00?buckets=512&tag=current", base + "/0?buckets=513&tag=current", base + "/0?buckets=512", base + "?"} {
		expectStatus(t, f.request(t, http.MethodGet, target, nil, headers), http.StatusBadRequest)
	}
	expectStatus(t, f.request(t, http.MethodGet, base+"/0?buckets=512&tag=obsolete", nil, headers), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, base+"/1?buckets=512&tag="+url.QueryEscape(artifact.ETag), nil, headers), http.StatusNotFound)
	if audioWaveformHTTPCounts(t, f) != before {
		t.Fatal("waveform delivery changed queue or playback state")
	}
	// An unscanned source edit preserves the file but makes its timeline unsafe.
	file, err := os.OpenFile(f.mediaPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.Write([]byte("source changed"))
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatal("mutate owned source", writeErr, closeErr)
	}
	stale := audioWaveformHTTPDescriptor(t, f, "", headers)
	if !reflect.DeepEqual(stale, map[string]any{"Available": false, "Stale": true}) {
		t.Fatalf("stale timeline was exposed: %#v", stale)
	}
	expectStatus(t, f.request(t, http.MethodGet, finestURL, nil, headers), http.StatusNotFound)
	if audioWaveformHTTPCounts(t, f) != before {
		t.Fatal("stale lookup queued automatic replacement")
	}
}

func TestHTTPAudioWaveformPermissionsAndApplicationProjection(t *testing.T) {
	f := newAudioWaveformHTTPFixture(t)
	before := audioWaveformHTTPCounts(t, f)
	headers := http.Header{"X-Emby-Token": {f.viewerToken}}
	base := "/emby/Items/" + f.itemID + "/AudioWaveforms"
	expectStatus(t, f.request(t, http.MethodGet, base, nil, nil), http.StatusUnauthorized)
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Items/missing/AudioWaveforms", nil, headers), http.StatusNotFound)
	writeAPIMediaFile(t, f.root, "waveform-shows/Example/Season 01/Example S01E01.mp4")
	television := createAndScanAPILibrary(t, f.serverFixture, f.cookie, f.csrf, filepath.Join(f.root, "waveform-shows"), "tvshows")
	var series string
	if err := f.pool.QueryRow(f.ctx, `SELECT id FROM items WHERE library_id=$1 AND type='Series' LIMIT 1`, television).Scan(&series); err != nil {
		t.Fatal(err)
	}
	response := f.request(t, http.MethodGet, "/emby/Items/"+series+"/AudioWaveforms", nil, headers)
	expectStatus(t, response, http.StatusOK)
	if !reflect.DeepEqual(jsonObject(t, response), map[string]any{"Available": false}) || audioWaveformHTTPCounts(t, f) != before {
		t.Fatal("Series fallback generated source work")
	}
	artifact := publishAudioWaveformHTTPArtifact(t, f)
	level := base + "/0?buckets=512&tag=" + url.QueryEscape(artifact.ETag)
	setHTTPUserPolicy(t, f.serverFixture, f.viewerID, `{"EnableAllFolders":true,"EnableMediaPlayback":false}`)
	expectStatus(t, f.request(t, http.MethodGet, base, nil, headers), http.StatusForbidden)
	expectStatus(t, f.request(t, http.MethodGet, level, nil, headers), http.StatusForbidden)
	setHTTPUserPolicy(t, f.serverFixture, f.viewerID, `{"EnableAllFolders":false,"EnabledFolders":[],"EnableMediaPlayback":true}`)
	expectStatus(t, f.request(t, http.MethodGet, base, nil, headers), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, level, nil, headers), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, base+"?UserId="+f.adminID, nil, headers), http.StatusForbidden)
	keys := applicationMediaIssueKeys(t, f.serverFixture, f.adminToken)
	before = audioWaveformHTTPCounts(t, f)
	// An explicit target narrows catalog visibility. Its playback authority
	// remains separate from the key, but its library ACL must still apply.
	expectStatus(t, f.request(t, http.MethodGet, base+"?UserId="+f.viewerID, nil, keys[0].headers), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, level+"&UserId="+f.viewerID, nil, keys[0].headers), http.StatusNotFound)
	setHTTPUserPolicy(t, f.serverFixture, f.viewerID, `{"EnableAllFolders":true,"EnableMediaPlayback":false}`)
	expectStatus(t, f.request(t, http.MethodGet, level, nil, headers), http.StatusForbidden)
	for _, projection := range []string{"", "?UserId=" + f.viewerID} {
		descriptor := audioWaveformHTTPDescriptor(t, f, projection, keys[0].headers)
		if descriptor["Available"] != true {
			t.Fatal("visible target's playback policy replaced independent application authority")
		}
		streams := descriptor["Streams"].([]any)
		levels := streams[0].(map[string]any)["Levels"].([]any)
		target := levels[0].(map[string]any)["Url"].(string)
		parsed, err := url.Parse(target)
		if err != nil || projection != "" && parsed.Query().Get("UserId") != f.viewerID {
			t.Fatal("level URL lost its explicit state projection")
		}
		expectStatus(t, f.request(t, http.MethodGet, target, nil, keys[0].headers), http.StatusOK)
	}
	expectStatus(t, f.request(t, http.MethodGet, base+"?UserId=unknown-target", nil, keys[0].headers), http.StatusNotFound)
	if _, err := f.users.RevokeApplicationKey(f.ctx, keys[1].principal, keys[0].key.ID); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.request(t, http.MethodGet, level, nil, keys[0].headers), http.StatusUnauthorized)
	if audioWaveformHTTPCounts(t, f) != before {
		t.Fatal("application waveform projection wrote user history or generation state")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE items SET media=NULL WHERE id=$1`, f.itemID); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(audioWaveformHTTPDescriptor(t, f, "", http.Header{"X-Emby-Token": {f.adminToken}}), map[string]any{"Available": false}) {
		t.Fatal("unprobed source did not use an ordinary fallback")
	}
}

func TestHTTPAudioWaveformAdminAdmissionReceiptsAndForceAreExplicit(t *testing.T) {
	f := newAudioWaveformHTTPFixture(t)
	path := "/admin/v1/media-analysis/runs"
	body := map[string]any{"Kind": "waveform", "RequestId": "waveform-http-retry", "LibraryIds": []string{f.libraryID}, "ItemIds": []string{f.itemID}, "Force": true}
	headers := http.Header{"X-CSRF-Token": {f.csrf}}
	before := audioWaveformHTTPCounts(t, f)
	for _, target := range []string{"/admin/v1/audio-waveforms/items", "/admin/v1/items/" + f.itemID + "/audio-waveforms"} {
		expectStatus(t, f.request(t, http.MethodGet, target, nil, nil), http.StatusUnauthorized)
		expectStatus(t, f.request(t, http.MethodGet, target, nil, http.Header{"X-Emby-Token": {f.adminToken}}), http.StatusUnauthorized)
		expectStatus(t, f.request(t, http.MethodGet, target, nil, nil, f.cookie), http.StatusOK)
	}
	for _, query := range []string{"?", "?Unknown=1", "?Limit=0", "?Limit=1&Limit=2", "?SearchTerm=%00", "?SearchTerm=%ff"} {
		expectStatus(t, f.request(t, http.MethodGet, "/admin/v1/audio-waveforms/items"+query, nil, nil, f.cookie), http.StatusBadRequest)
	}
	expectStatus(t, f.request(t, http.MethodPost, path, body, nil, f.cookie), http.StatusForbidden)
	expectStatus(t, f.request(t, http.MethodPost, path, body, http.Header{"X-CSRF-Token": {f.csrf}, "Origin": {"https://outside.example"}}, f.cookie), http.StatusForbidden)
	f.app.audioWaveforms = nil
	expectStatus(t, f.request(t, http.MethodPost, path, body, headers, f.cookie), http.StatusServiceUnavailable)
	if audioWaveformHTTPCounts(t, f) != before {
		t.Fatal("unavailable or unauthenticated request queued waveforms")
	}
	// A no-source executor isolates real admission/receipts from signal
	// extraction. The runtime flag enables only this fixture's admission gate.
	if err := f.app.taskManager.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	registry, err := tasks.NewExecutorRegistry(tasks.ExecutorRegistration{Key: library.TaskAudioWaveformGenerationKey, Name: "Waveform HTTP admission fixture", Executor: analysisHTTPNoWorkExecutor{}})
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
	f.app.audioWaveforms = &audioWaveformRuntime{available: true}
	response := f.request(t, http.MethodPost, path, body, headers, f.cookie)
	expectStatus(t, response, http.StatusAccepted)
	first := jsonObject(t, response)
	if first["Queued"] != float64(1) {
		t.Fatalf("selection did not queue one source: %#v", first)
	}
	repeated := f.request(t, http.MethodPost, path, body, headers, f.cookie)
	expectStatus(t, repeated, http.StatusAccepted)
	receipt := jsonObject(t, repeated)
	if receipt["RunId"] != first["RunId"] || receipt["TaskId"] != first["TaskId"] || receipt["Admitted"] != false || receipt["Queued"] != float64(1) {
		t.Fatal("HTTP retry did not retain its durable receipt")
	}
	var count, revision int64
	var force bool
	var actor string
	if err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM audio_waveform_requests),requested_revision,force,actor_user_id FROM audio_waveform_queue WHERE item_id=$1`, f.itemID).Scan(&count, &revision, &force, &actor); err != nil || count != 1 || revision != 1 || !force || actor != f.adminID {
		t.Fatal("HTTP admission lost exact selection, Force or native actor", err)
	}
	body["Force"] = false
	expectAPIError(t, f.request(t, http.MethodPost, path, body, headers, f.cookie), http.StatusConflict, "audio_waveform_conflict", false)
	if counts := audioWaveformHTTPCounts(t, f); counts[0] != before[0]+1 || counts[1] != before[1]+1 || counts[2] != before[2] || counts[3] != before[3] || counts[4] != before[4] {
		t.Fatal("admission replay generated duplicate work or playback history")
	}
}
