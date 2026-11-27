//go:build linux

package server

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
)

func TestDolbyVisionFastIngestPublishesWhileBlockedAndRecoversAfterRestart(t *testing.T) {
	f, accounts := newClientSessionHTTPAccounts(t, 2*time.Minute)
	root, tools := t.TempDir(), t.TempDir()
	probe, ffmpeg, gate := filepath.Join(tools, "probe"), filepath.Join(tools, "ffmpeg"), filepath.Join(tools, "release")
	writeFastIngestFile(t, probe, []byte(`#!/bin/sh
case "$*" in
*-show_streams*)
    printf 'probe\n' >> "$0.calls"
    printf '%s\n' '{"format":{"format_name":"mov,mp4","duration":"4","start_time":"0"},"streams":[{"index":7,"codec_type":"video","codec_name":"hevc","profile":"Main 10","width":320,"height":180,"pix_fmt":"yuv420p10le","time_base":"1/1000","side_data_list":[{"side_data_type":"DOVI configuration record","dv_profile":8,"dv_level":6,"rpu_present_flag":1,"el_present_flag":0,"bl_present_flag":1,"dv_bl_signal_compatibility_id":1,"dv_md_compression":"none"}]}]}' ;;
*) exit 1 ;;
esac
`), 0700)
	writeFastIngestFile(t, ffmpeg, []byte(fmt.Sprintf(`#!/bin/sh
set -eu
case " $* " in
*" -i /proc/self/fd/3 "*) ;;
*) exit 1 ;;
esac
case " $* " in
*" -map 0:7 "*) ;;
*) exit 1 ;;
esac
case "$*" in
*dovi_rpu=compression=none*)
    printf 'validate\n' >> "$0.calls"
    while [ ! -f %q ]; do /bin/sleep 0.05; done
    test -s /proc/self/fd/3 ;;
*filter_units*)
    printf 'extract\n' >> "$0.calls"
    /bin/cat /proc/self/fd/3 ;;
*) exit 1 ;;
esac
`, gate)), 0700)
	// One synthetic AUD+RPU access unit with mapping data and a valid CRC.
	// The fixture tool models the native process boundary, not HEVC decoding.
	accessUnit, err := hex.DecodeString("00000001460110000000017c011908000840e4d9433c00ffe007ff003fff40008001a0004000d000200000504a11c380")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "Deferred Dolby Vision.mp4")
	writeFastIngestFile(t, path, accessUnit, 0600)
	cfg := f.cfg
	cfg.FFprobePath, cfg.FFmpegPath, cfg.MediaRoots, cfg.FastLibraryIngest = probe, ffmpeg, []string{root}, true
	fastIngestReopen(t, f, cfg)
	collection, err := f.app.library.CreateLibrary(f.ctx, "Deferred Dolby Vision", "movies", []string{root})
	if err != nil {
		t.Fatal(err)
	}
	waitRunning := func(calls string) string {
		t.Helper()
		var runID string
		scheduledTaskHTTPWait(t, &scheduledTaskHTTPFixture{serverFixture: f}, "blocked Dolby Vision validation pass", func() bool {
			def, err := f.app.taskStore.GetByKey(f.ctx, library.TaskDolbyVisionVerificationKey)
			if err != nil {
				t.Fatal(err)
			}
			log, _ := os.ReadFile(ffmpeg + ".calls")
			if def.CurrentRun == nil && def.LastRun != nil && def.LastRun.TotalChildren > 0 && def.LastRun.State != tasks.RunInterrupted {
				t.Fatalf("verification finished before the blocked pass: %s (%s), calls=%q", def.LastRun.State, def.LastRun.ErrorCode, log)
			}
			if string(log) != calls || def.CurrentRun == nil || def.CurrentRun.State != tasks.RunRunning {
				return false
			}
			runID = def.CurrentRun.ID
			return true
		})
		return runID
	}
	(&streamHTTPFixture{f: f}).rescan(t, collection.ID)
	firstRun := waitRunning("extract\nvalidate\n")
	item := fastIngestItem(t, f, accounts.viewer.userID, collection.ID, path)
	if item.Media == nil || len(item.Media.Streams) != 1 || item.Media.Streams[0].VideoRange != "DOVI" ||
		item.Media.Streams[0].DolbyVision == nil || item.Media.Streams[0].DolbyVision.RPUVerified {
		t.Fatal("completed fast scan did not publish the unverified Dolby Vision declaration")
	}
	getStatus := func(want string) library.AdminDolbyVisionVerification {
		t.Helper()
		response := f.request(t, http.MethodGet, "/admin/v1/items/"+item.ID+"/media-info", nil, nil, accounts.cookie)
		expectStatus(t, response, http.StatusOK)
		var status library.AdminMediaInfo
		if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil || len(status.DolbyVisionVerification) != 1 {
			t.Fatalf("read stream-scoped Dolby Vision status: %+v, %v", status.DolbyVisionVerification, err)
		}
		state := status.DolbyVisionVerification[0]
		if state.State != want || state.StreamIndex != 7 || (state.VerifiedAt != nil) != (want == "verified") ||
			bytes.Contains(response.Body.Bytes(), []byte("ffmpeg-file-v1:")) || bytes.Contains(response.Body.Bytes(), []byte("ResidualDisabled")) {
			t.Fatalf("incorrect public verification status: %+v", state)
		}
		return state
	}
	getStatus("pending")
	original := "/emby/Videos/" + item.ID + "/original.mp4"
	response := f.request(t, http.MethodGet, original, nil, accounts.viewer.headers)
	expectStatus(t, response, http.StatusOK)
	if !bytes.Equal(response.Body.Bytes(), accessUnit) || response.Header().Get("ETag") == "" {
		t.Fatal("verification blocked or changed the authorized original stream")
	}
	originalETag := response.Header().Get("ETag")
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Users/"+accounts.viewer.userID+"/FavoriteItems/"+item.ID, nil, accounts.viewer.headers), http.StatusOK)
	stamp := fastIngestSourceStamp(t, f, item.ID)

	// Reopen while the second pass is blocked. The durable queue must recover
	// under a new run without publishing proof or modifying the original.
	fastIngestReopen(t, f, cfg)
	recovered := waitRunning("extract\nvalidate\nextract\nvalidate\n")
	if recovered == firstRun || fastIngestSourceStamp(t, f, item.ID) != stamp {
		t.Fatal("recovery reused a dead run or changed the source stamp")
	}
	getStatus("pending")
	response = f.request(t, http.MethodGet, original, nil, accounts.viewer.headers)
	expectStatus(t, response, http.StatusOK)
	if response.Header().Get("ETag") != originalETag {
		t.Fatal("restarting with pending evidence changed the original ETag")
	}

	// Replace the name while the verifier has the old descriptor. Releasing
	// its native pass cannot attest to the new file or publish stale proof.
	replacement := bytes.Repeat(accessUnit, 2)
	writeFastIngestFile(t, path+".replacement", replacement, 0600)
	if err := os.Rename(path+".replacement", path); err != nil {
		t.Fatal(err)
	}
	writeFastIngestFile(t, gate, []byte("allow validation"), 0600)
	fastIngestWaitRun(t, f, recovered, tasks.RunCompleted)
	var pending bool
	if err := f.pool.QueryRow(f.ctx, `SELECT evidence IS NULL AND verified_at IS NULL AND attempts=0 AND source_revision=$2
		FROM dolby_vision_verifications WHERE item_id=$1 AND stream_index=7`, item.ID, stamp).Scan(&pending); err != nil || !pending {
		t.Fatalf("a replaced source result was published or consumed its retry budget: pending=%v, %v", pending, err)
	}

	(&streamHTTPFixture{f: f}).rescan(t, collection.ID)
	var stored media.DolbyVisionRPUEvidence
	defer func() {
		if t.Failed() {
			def, _ := f.app.taskStore.GetByKey(f.ctx, library.TaskDolbyVisionVerificationKey)
			calls, _ := os.ReadFile(ffmpeg + ".calls")
			var attempts int
			var reason, scanState string
			var matching bool
			err := f.pool.QueryRow(f.ctx, `SELECT q.attempts,COALESCE(q.evidence->>'Reason',''),j.status,
				q.source_revision=`+library.MediaOperationSourceRevisionSQL+` FROM dolby_vision_verifications q
				JOIN scan_jobs j ON j.id=q.scan_job_id JOIN items i ON i.id=q.item_id WHERE i.id=$1 AND q.stream_index=7`, item.ID).
				Scan(&attempts, &reason, &scanState, &matching)
			t.Logf("verification queue: attempts=%d reason=%q scan=%q current=%v err=%v; calls=%q, active=%+v, previous=%+v",
				attempts, reason, scanState, matching, err, calls, def.CurrentRun, def.LastRun)
		}
	}()
	scheduledTaskHTTPWait(t, &scheduledTaskHTTPFixture{serverFixture: f}, "durable Dolby Vision verification of the replacement", func() bool {
		var result []byte
		if err := f.pool.QueryRow(f.ctx, `SELECT evidence FROM dolby_vision_verifications WHERE item_id=$1 AND stream_index=7`, item.ID).Scan(&result); err != nil {
			t.Fatal(err)
		}
		return result != nil && json.Unmarshal(result, &stored) == nil && stored.Verified
	})
	if !stored.Valid() || stored.Profile != 8 || stored.StreamIndex != 7 || stored.FrameCount != 2 || !stored.ResidualDisabled {
		t.Fatalf("worker did not store complete proof for the replacement: %+v", stored)
	}
	verifiedAt := getStatus("verified").VerifiedAt
	itemAfter := fastIngestItem(t, f, accounts.viewer.userID, collection.ID, path)
	if itemAfter.ID != item.ID || itemAfter.UserData == nil || !itemAfter.UserData.IsFavorite ||
		itemAfter.Media == nil || itemAfter.Media.Streams[0].DolbyVision.RPUVerified || fastIngestSourceStamp(t, f, item.ID) == stamp {
		t.Fatal("replacement verification did not preserve the item and history or wrote into its media document")
	}
	response = f.request(t, http.MethodGet, original, nil, accounts.viewer.headers)
	expectStatus(t, response, http.StatusOK)
	if response.Header().Get("ETag") == originalETag || !bytes.Equal(response.Body.Bytes(), replacement) {
		t.Fatal("original route did not return the replacement under its own ETag")
	}
	currentETag := response.Header().Get("ETag")
	calls := "extract\nvalidate\nextract\nvalidate\nextract\nvalidate\n"
	(&streamHTTPFixture{f: f}).rescan(t, collection.ID)
	if status := getStatus("verified"); !status.VerifiedAt.Equal(*verifiedAt) || fastIngestToolLog(t, probe+".calls") != "probe\nprobe\n" ||
		fastIngestToolLog(t, ffmpeg+".calls") != calls {
		t.Fatal("warm scan reprobed or reverified an unchanged source")
	}

	// Disabling deferral does not invalidate completed evidence. A forced full
	// refresh still runs verification synchronously for the same file.
	cfg.FastLibraryIngest = false
	fastIngestReopen(t, f, cfg)
	getStatus("verified")
	job, err := f.app.library.StartScanWithOptions(f.ctx, collection.ID, library.ScanOptions{ForceProbe: true})
	if err != nil {
		t.Fatal(err)
	}
	scheduledTaskHTTPWait(t, &scheduledTaskHTTPFixture{serverFixture: f}, "complete synchronous Dolby Vision refresh", func() bool {
		current, err := f.app.library.GetJob(f.ctx, job.ID)
		if err != nil || current.Status == "Failed" || current.Status == "Cancelled" || current.Status == "Interrupted" || current.Error != "" {
			t.Fatalf("forced refresh failed: %+v, %v", current, err)
		}
		return current.Status == "Completed"
	})
	itemAfter = fastIngestItem(t, f, accounts.viewer.userID, collection.ID, path)
	if itemAfter.Media == nil || !itemAfter.Media.Streams[0].DolbyVision.RPUVerified ||
		fastIngestToolLog(t, ffmpeg+".calls") != calls+"extract\nvalidate\n" {
		t.Fatal("the explicit refresh did not use the original full probe")
	}
	response = f.request(t, http.MethodGet, original, nil, accounts.viewer.headers)
	expectStatus(t, response, http.StatusOK)
	if response.Header().Get("ETag") != currentETag {
		t.Fatal("synchronous proof changed the unchanged file's original ETag")
	}
}
