//go:build linux

package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/creditsskipper"
	"github.com/moooyo/goby/internal/introskipper"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
)

type creditsHTTPProbe struct{ black bool }

func (p creditsHTTPProbe) ScanKeyframes(_ context.Context, window creditsskipper.Range, _ int) (creditsskipper.KeyframeEvidence, error) {
	result := creditsskipper.KeyframeEvidence{}
	if p.black {
		for second := 0; float64(second) < window.Duration(); second++ {
			percentage := 10
			if float64(second) >= window.Duration()-30 {
				percentage = 95
			}
			result.BlackFrames = append(result.BlackFrames, creditsskipper.BlackFrame{Frame: second, Time: float64(second), Percentage: percentage})
		}
	}
	return result, nil
}
func (creditsHTTPProbe) ScanBlackIntervals(context.Context, creditsskipper.Range, int, int) ([]creditsskipper.Range, error) {
	return nil, nil
}
func (creditsHTTPProbe) ScanBoundary(context.Context, creditsskipper.Range, int, int) ([]creditsskipper.BlackFrame, error) {
	return nil, nil
}
func (creditsHTTPProbe) ScanSilence(context.Context, creditsskipper.Range) ([]creditsskipper.Range, error) {
	return nil, nil
}
func (creditsHTTPProbe) ScanKeyframesAtBoundary(context.Context, creditsskipper.Range) ([]float64, error) {
	return nil, nil
}

type creditsHTTPOutcome struct {
	work   library.AnalysisWork
	values map[string]library.AnalysisStoredCreditsResult
	err    error
}

type creditsHTTPExecutor struct {
	fixture analysisProjectionFixture
	mode    string
	done    chan creditsHTTPOutcome
}

func (*creditsHTTPExecutor) Available() bool { return true }

// The real task manager supplies the sealed fence and immutable cohort. Only
// detector inputs are deterministic: this verifies publication and HTTP output,
// without claiming that FFmpeg or Chromaprint executed in this fixture.
func (e *creditsHTTPExecutor) Execute(ctx context.Context, task tasks.Work, report func(tasks.Progress) error) (resultErr error) {
	outcome := creditsHTTPOutcome{}
	defer func() { outcome.err = resultErr; e.done <- outcome }()
	work, err := e.fixture.app.library.GetAnalysisWork(ctx, task.ChildID, task.Fence)
	if err != nil {
		return err
	}
	outcome.work = work
	values := map[string]library.AnalysisStoredCreditsResult{}
	hashes := map[string]string{}
	if e.mode == "audio" {
		for _, source := range work.Sources {
			var path string
			if err := e.fixture.pool.QueryRow(ctx, `SELECT path FROM items WHERE id=$1`, source.ItemID).Scan(&path); err != nil {
				return err
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			hash := sha256.Sum256(content)
			hashes[source.ItemID] = hex.EncodeToString(hash[:])
		}
	}
	for _, source := range work.Sources {
		if !source.Target {
			continue
		}
		duration := float64(source.DurationTicks) / float64(media.TicksPerSecond)
		request := creditsskipper.Request{DurationSeconds: duration, IsMovie: source.ItemType == "Movie"}
		value := library.AnalysisStoredCreditsResult{Version: library.AnalysisCreditsResultVersion, ItemID: source.ItemID, SourceRevision: source.SourceRevision, DurationTicks: source.DurationTicks, Segments: []library.CreditsInterval{}}
		if e.mode == "multiple" {
			request.Chapters = []creditsskipper.Chapter{{Name: "Main", StartSeconds: 0}, {Name: "Credits", StartSeconds: duration - 100}, {Name: "Content", StartSeconds: duration - 60}}
		} else if e.mode == "audio" {
			interval := introskipper.Interval{StartTicks: source.DurationTicks - 40*media.TicksPerSecond, EndTicks: source.DurationTicks - 5*media.TicksPerSecond}
			pair := []introskipper.Support{{EpisodeKey: source.EpisodeKey, SourceKey: source.SourceRevision, ContentIdentity: hashes[source.ItemID], AlgorithmProfile: work.Execution.IntroProfile, Interval: interval}}
			for _, member := range work.Sources {
				if member.ItemID == source.ItemID {
					continue
				}
				pair = append(pair, introskipper.Support{EpisodeKey: member.EpisodeKey, SourceKey: member.SourceRevision, ContentIdentity: hashes[member.ItemID], AlgorithmProfile: work.Execution.IntroProfile, Interval: interval})
				break
			}
			if len(pair) != 2 {
				return fmt.Errorf("credits HTTP fixture requires an independent pair")
			}
			value.Audio = &introskipper.EpisodeResult{EpisodeKey: source.EpisodeKey, SourceKey: source.SourceRevision, ContentIdentity: hashes[source.ItemID], AlgorithmProfile: work.Execution.IntroProfile, DurationTicks: source.DurationTicks,
				Status: introskipper.Qualified, Reasons: []string{}, Candidate: &introskipper.Candidate{Interval: interval, UpstreamCommit: introskipper.UpstreamCommit, Support: pair}}
			request.AudioSegments = []creditsskipper.Segment{{Start: duration - 40, End: duration - 5, Source: creditsskipper.ChromaprintSource}}
		}
		visual, err := creditsskipper.Detect(ctx, request, creditsHTTPProbe{black: e.mode == "multiple"})
		if err != nil {
			return err
		}
		value.Visual = &visual
		for _, segment := range visual.Segments {
			value.Segments = append(value.Segments, library.CreditsInterval{StartTicks: int64(segment.Start * float64(media.TicksPerSecond)), EndTicks: int64(segment.End * float64(media.TicksPerSecond)), Source: string(segment.Source)})
		}
		if len(value.Segments) == 0 {
			value.Reason = "no_credits_detected"
		}
		values[source.ItemID] = value
	}
	outcome.values = values
	if err := e.fixture.app.library.PublishCreditsAnalysis(ctx, task.ChildID, task.Fence, values, hashes); err != nil {
		return err
	}
	return report(tasks.Progress{Processed: int64(len(work.Sources)), Updated: int64(len(values))})
}

func newCreditsHTTPFixture(t *testing.T, mode string) (analysisProjectionFixture, *creditsHTTPExecutor) {
	t.Helper()
	f := newAnalysisProjectionFixture(t)
	if _, err := f.pool.Exec(f.ctx, `UPDATE libraries SET options=options||'{"EnableCreditsDetection":true}'::jsonb WHERE id=$1`, f.libraryID); err != nil {
		t.Fatal(err)
	}
	execution := library.AnalysisExecutionProfile{Version: library.AnalysisExecutionProfileVersion, Available: true,
		FFmpegSHA256: strings.Repeat("a", 64), FFprobeSHA256: strings.Repeat("b", 64), FingerprintSHA256: strings.Repeat("c", 64),
		DetectorVersion: introskipper.CreditsVersion, IntroSkipperOptions: introskipper.DefaultOptions(), IntroProfile: "credits-http-deterministic-evidence-v1"}
	executor := &creditsHTTPExecutor{fixture: f, mode: mode, done: make(chan creditsHTTPOutcome, 1)}
	registry, err := tasks.NewExecutorRegistry(tasks.ExecutorRegistration{Key: library.TaskCreditsAnalysisKey, Name: "Credits HTTP evidence fixture", Executor: executor,
		AnalysisAdmission: func(tx library.OwnedTx, request tasks.AnalysisAdmissionRequest) (tasks.AnalysisAdmissionBinding, error) {
			binding, err := library.PrepareAnalysis(tx, request.TaskKey, request.Selection, execution)
			return tasks.AnalysisAdmissionBinding{ConfigurationFingerprint: binding.ConfigurationFingerprint, Bind: binding.Bind, SnapshotChildren: binding.SnapshotChildren}, err
		}})
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
	// The base fixture's intro-only registry disabled the credits definition.
	// Reconciliation correctly preserves that choice. Explicitly enable only
	// this installed deterministic executor before starting its task manager.
	if err := f.app.library.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error {
		tag, err := tx.Exec(`UPDATE task_definitions SET enabled=true,revision=revision+1,updated_at=clock_timestamp() WHERE key=$1 AND NOT enabled`, library.TaskCreditsAnalysisKey)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("credits fixture expected one disabled definition to enable")
		}
		return nil
	}); err != nil {
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
	return f, executor
}

func runCreditsHTTPFixture(t *testing.T, f analysisProjectionFixture, executor *creditsHTTPExecutor) creditsHTTPOutcome {
	t.Helper()
	body := map[string]any{"Kind": "credits", "RequestId": "credits-http-request", "LibraryIds": []string{f.libraryID}, "ItemIds": []string{f.ids[0]}, "Force": false}
	response := f.request(t, http.MethodPost, "/admin/v1/media-analysis/runs", body, http.Header{"X-CSRF-Token": {f.csrf}}, f.cookie)
	expectStatus(t, response, http.StatusAccepted)
	first := jsonObject(t, response)
	if len(first) != 3 || first["RunId"] == nil || first["TaskId"] == nil || first["Admitted"] != true {
		t.Fatalf("credits must retain classic analysis admission shape: %#v", first)
	}
	select {
	case result := <-executor.done:
		if result.err != nil {
			t.Fatal("publish deterministic credits through the task fence", result.err)
		}
		replay := f.request(t, http.MethodPost, "/admin/v1/media-analysis/runs", body, http.Header{"X-CSRF-Token": {f.csrf}}, f.cookie)
		expectStatus(t, replay, http.StatusAccepted)
		receipt := jsonObject(t, replay)
		if len(receipt) != 3 || receipt["RunId"] != first["RunId"] || receipt["Admitted"] != false {
			t.Fatal("credits replay lost its analysis receipt")
		}
		return result
	case <-time.After(15 * time.Second):
		t.Fatal("credits HTTP fixture publication timed out")
	case <-f.ctx.Done():
		t.Fatal(f.ctx.Err())
	}
	return creditsHTTPOutcome{}
}

func assertCreditsHTTPIntervals(t *testing.T, f analysisProjectionFixture, want []map[string]any) [4]int64 {
	t.Helper()
	headers := http.Header{"X-Emby-Token": {f.token}}
	beforeDetail := creditsHTTPReadCounts(t, f)
	detail := f.request(t, http.MethodGet, "/emby/Users/"+f.actor.User.ID+"/Items/"+f.ids[0], nil, headers)
	expectStatus(t, detail, http.StatusOK)
	if after := creditsHTTPReadCounts(t, f); after != beforeDetail {
		t.Fatalf("pure item GET changed [runs, detection revisions, user-data rows, encoders]: before=%v after=%v", beforeDetail, after)
	}
	playback := f.request(t, http.MethodGet, "/emby/Items/"+f.ids[0]+"/PlaybackInfo", nil, headers)
	expectStatus(t, playback, http.StatusOK)
	afterNegotiation := creditsHTTPReadCounts(t, f)
	// PlaybackInfo negotiates a Prepared session and initializes its neutral
	// user-data row. It must not admit analysis, publish detections, run an
	// encoder, or record any viewing activity. Pure item GET remains stricter.
	if afterNegotiation[0] != beforeDetail[0] || afterNegotiation[1] != beforeDetail[1] || afterNegotiation[3] != beforeDetail[3] ||
		afterNegotiation[2] != max(int64(1), beforeDetail[2]) {
		t.Fatalf("PlaybackInfo changed more than its neutral user-data row [runs, detection revisions, rows, encoders]: before=%v after=%v", beforeDetail, afterNegotiation)
	}
	playbackObject := jsonObject(t, playback)
	var neutralRow, prepared bool
	if err := f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM user_item_data WHERE user_id=$1 AND item_id=$2
		AND playback_position_ticks=0 AND play_count=0 AND NOT played AND last_played_at IS NULL AND NOT is_favorite
		AND rating IS NULL AND likes IS NULL AND NOT hide_from_resume),
		EXISTS(SELECT 1 FROM play_sessions WHERE id=$3 AND user_id=$1 AND item_id=$2 AND state='Prepared'
		AND position_ticks=0 AND NOT counted AND started_at IS NULL AND stopped_at IS NULL)`,
		f.actor.User.ID, f.ids[0], stringValue(t, playbackObject, "PlaySessionId")).Scan(&neutralRow, &prepared); err != nil || !neutralRow || !prepared {
		t.Fatalf("credits projection recorded playback activity during negotiation: neutral-row=%t prepared=%t error=%v", neutralRow, prepared, err)
	}
	sources, ok := playbackObject["MediaSources"].([]any)
	if !ok || len(sources) != 1 {
		t.Fatal("playback omitted the real original source")
	}
	for _, object := range []map[string]any{jsonObject(t, detail), sources[0].(map[string]any)} {
		intervals, ok := object["GobyCreditsIntervals"].([]any)
		if !ok || len(intervals) != len(want) {
			t.Fatalf("authoritative interval array missing: %#v", object["GobyCreditsIntervals"])
		}
		for index, expected := range want {
			actual := intervals[index].(map[string]any)
			if actual["StartPositionTicks"] != float64(expected["StartPositionTicks"].(int64)) || actual["EndPositionTicks"] != float64(expected["EndPositionTicks"].(int64)) || actual["Source"] != expected["Source"] {
				t.Fatalf("interval changed: %#v expected %#v", actual, expected)
			}
		}
		markers := 0
		for _, raw := range object["Chapters"].([]any) {
			chapter := raw.(map[string]any)
			if chapter["MarkerType"] == "CreditsEnd" {
				t.Fatal("unknown standard CreditsEnd marker was emitted")
			}
			if chapter["MarkerType"] == "CreditsStart" {
				markers++
				if len(want) == 0 || chapter["StartPositionTicks"] != float64(want[0]["StartPositionTicks"].(int64)) {
					t.Fatal("standard marker did not use the first effective start")
				}
			}
		}
		if len(want) == 0 && markers != 0 || len(want) > 0 && markers != 1 {
			t.Fatal("standard credits boundaries were duplicated or omitted")
		}
	}
	return afterNegotiation
}

func creditsHTTPReadCounts(t *testing.T, f analysisProjectionFixture) [4]int64 {
	t.Helper()
	var result [4]int64
	if err := f.pool.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM task_runs),(SELECT COALESCE(sum(revision),0) FROM analysis_credits_detections),
		(SELECT count(*) FROM user_item_data),(SELECT count(*) FROM encoding_jobs)`).Scan(&result[0], &result[1], &result[2], &result[3]); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestAnalysisCreditsHTTPMultiIntervalAndNativeManualCompatibility(t *testing.T) {
	f, executor := newCreditsHTTPFixture(t, "multiple")
	path := "/admin/v1/items/" + f.ids[0] + "/credits"
	body := map[string]any{"Kind": "credits", "RequestId": "denied", "LibraryIds": []string{f.libraryID}, "ItemIds": []string{f.ids[0]}, "Force": false}
	expectStatus(t, f.request(t, http.MethodPost, "/admin/v1/media-analysis/runs", body, nil, f.cookie), http.StatusForbidden)
	expectStatus(t, f.request(t, http.MethodGet, path, nil, http.Header{"X-Emby-Token": {f.token}}), http.StatusUnauthorized)
	result := runCreditsHTTPFixture(t, f, executor)
	segments := result.values[f.ids[0]].Segments
	if len(segments) != 2 || segments[0].EndTicks >= segments[1].StartTicks {
		t.Fatalf("deterministic fixture lacks a content gap: %+v", segments)
	}
	want := []map[string]any{}
	for _, segment := range segments {
		want = append(want, map[string]any{"StartPositionTicks": segment.StartTicks, "EndPositionTicks": segment.EndTicks, "Source": segment.Source})
	}
	before := assertCreditsHTTPIntervals(t, f, want)
	response := f.request(t, http.MethodGet, path, nil, nil, f.cookie)
	expectStatus(t, response, http.StatusOK)
	detail := jsonObject(t, response)
	if objectValue(t, detail, "Effective")["Provenance"] != "Detected" || detail["DetectedStale"] != false || len(detail["Detected"].([]any)) != 2 || detail["Override"] != nil {
		t.Fatalf("native automatic projection changed: %#v", detail)
	}
	start := result.work.Sources[0].DurationTicks - 10*media.TicksPerSecond
	edit := map[string]any{"Revision": detail["Revision"], "SourceRevision": detail["SourceRevision"], "StartTicks": start, "Provenance": "Manual"}
	headers := http.Header{"X-CSRF-Token": {f.csrf}}
	response = f.request(t, http.MethodPut, path, edit, headers, f.cookie)
	expectStatus(t, response, http.StatusOK)
	manual := jsonObject(t, response)
	if objectValue(t, manual, "Effective")["Provenance"] != "Manual" || len(manual["Detected"].([]any)) != 2 {
		t.Fatal("manual override lost retained automatic evidence")
	}
	assertCreditsHTTPIntervals(t, f, []map[string]any{{"StartPositionTicks": start, "EndPositionTicks": start + 10*media.TicksPerSecond, "Source": "Manual"}})
	response = f.request(t, http.MethodDelete, path, map[string]any{"Revision": manual["Revision"], "SourceRevision": manual["SourceRevision"]}, headers, f.cookie)
	expectStatus(t, response, http.StatusOK)
	if objectValue(t, jsonObject(t, response), "Effective")["Provenance"] != "Detected" {
		t.Fatal("clearing manual override failed to reveal the valid automatic result")
	}
	assertCreditsHTTPIntervals(t, f, want)
	if after := creditsHTTPReadCounts(t, f); after != before {
		t.Fatalf("passive projections/manual override changed [runs, detection revisions, user-data rows, encoders]: before=%v after=%v", before, after)
	}
}

func TestAnalysisCreditsHTTPEmptyAndChangedGraphsWithdrawMarkers(t *testing.T) {
	for _, change := range []string{"no-result", "source", "support-file", "cohort"} {
		t.Run(change, func(t *testing.T) {
			mode := "multiple"
			if change == "no-result" {
				mode = "none"
			}
			if change == "support-file" {
				mode = "audio"
			}
			f, executor := newCreditsHTTPFixture(t, mode)
			result := runCreditsHTTPFixture(t, f, executor)
			if change != "no-result" && len(result.values[f.ids[0]].Segments) == 0 {
				t.Fatal("qualified fixture had no result")
			}
			if change == "source" {
				if err := os.WriteFile(f.paths[0], []byte("changed physical credits source"), 0600); err != nil {
					t.Fatal(err)
				}
				f.rescan(t)
			}
			if change == "support-file" {
				peer := result.values[f.ids[0]].Audio.Candidate.Support[0].SourceKey
				if peer == result.values[f.ids[0]].SourceRevision {
					peer = result.values[f.ids[0]].Audio.Candidate.Support[1].SourceKey
				}
				for _, source := range result.work.Sources {
					if source.SourceRevision == peer {
						var path string
						if err := f.pool.QueryRow(f.ctx, `SELECT path FROM items WHERE id=$1`, source.ItemID).Scan(&path); err != nil {
							t.Fatal(err)
						}
						if err := os.WriteFile(path, []byte("changed independent credits support"), 0600); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			if change == "cohort" {
				if _, err := f.pool.Exec(f.ctx, `UPDATE items SET index_number=99 WHERE id=$1`, f.ids[2]); err != nil {
					t.Fatal(err)
				}
			}
			before := assertCreditsHTTPIntervals(t, f, []map[string]any{})
			response := f.request(t, http.MethodGet, "/admin/v1/items/"+f.ids[0]+"/credits", nil, nil, f.cookie)
			expectStatus(t, response, http.StatusOK)
			detail := jsonObject(t, response)
			if detail["Effective"] != nil || change != "no-result" && detail["DetectedStale"] != true || change == "no-result" && (detail["DetectedStatus"] != "no_result" || !reflect.DeepEqual(detail["Detected"], []any{})) {
				t.Fatalf("native stale/empty result contract changed: %#v", detail)
			}
			if after := creditsHTTPReadCounts(t, f); after != before {
				t.Fatalf("stale/empty read changed [runs, detection revisions, user-data rows, encoders]: before=%v after=%v", before, after)
			}
		})
	}
}
