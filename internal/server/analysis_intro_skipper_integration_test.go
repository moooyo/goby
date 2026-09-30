//go:build linux

package server

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/introskipper"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
)

type introSkipperExecutionStart struct {
	Task tasks.Work
	Work library.AnalysisWork
}

// This wrapper observes the manager's real sealed work capability. Its gate only
// makes a configuration-change race deterministic; the production executor owns
// extraction, cache reads/writes, matching, progress, and publication.
type introSkipperIntegrationExecutor struct {
	inner     mediaAnalysisTaskExecutor
	gates     chan chan struct{}
	started   chan introSkipperExecutionStart
	completed chan error
}

func (e *introSkipperIntegrationExecutor) Available() bool { return e.inner.Available() }
func (e *introSkipperIntegrationExecutor) Execute(ctx context.Context, task tasks.Work, progress func(tasks.Progress) error) error {
	work, err := e.inner.runtime.server.library.GetAnalysisWork(ctx, task.ChildID, task.Fence)
	if err != nil {
		return err
	}
	var gate chan struct{}
	select {
	case gate = <-e.gates:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case e.started <- introSkipperExecutionStart{task, work}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-gate:
	case <-ctx.Done():
		return ctx.Err()
	}
	err = e.inner.Execute(ctx, task, progress)
	e.completed <- err
	return err
}

type introSkipperServerFixture struct {
	analysisProjectionFixture
	executor     *introSkipperIntegrationExecutor
	definition   tasks.Definition
	extractionFD int
	extractions  *int
}

func newIntroSkipperServerFixture(t *testing.T) introSkipperServerFixture {
	t.Helper()
	ffmpeg, ffprobe, introFFmpeg := os.Getenv("GOBY_FFMPEG"), os.Getenv("GOBY_FFPROBE"), os.Getenv("GOBY_INTRO_SKIPPER_FFMPEG")
	if ffmpeg == "" || ffprobe == "" || introFFmpeg == "" {
		t.Skip("set GOBY_FFMPEG, GOBY_FFPROBE, and GOBY_INTRO_SKIPPER_FFMPEG for the real native intro pipeline")
	}
	f := newServerFixtureWithTimeout(t, 3*time.Minute)
	f.bootstrap(t)
	if err := f.app.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	season := filepath.Join(root, "tv", "Native Intro Show", "Season 01")
	if err := os.MkdirAll(season, 0700); err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 2)
	for index, color := range []string{"red", "blue"} {
		paths[index] = filepath.Join(season, fmt.Sprintf("Native.Intro.Show.S01E%02d.mkv", index+1))
		// Different video bytes provide distinct full-content identities while
		// the same decoded audio exercises the upstream pair matcher. These
		// generated sources are mechanics fixtures, not accuracy evidence.
		hlsHTTPMediaCommand(t, ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-filter_threads", "1",
			"-f", "lavfi", "-i", "color=c="+color+":size=64x64:rate=1:duration=40",
			"-f", "lavfi", "-i", "aevalsrc=0.3*sin(2*PI*(220+mod(floor(t)\\,7)*37)*t):s=11025:d=40",
			"-map", "0:v:0", "-map", "1:a:0", "-c:v", "libx264", "-threads:v", "1", "-preset", "ultrafast",
			"-pix_fmt", "yuv420p", "-c:a", "pcm_s16le", "-threads:a", "1", "-t", "40", paths[index])
	}
	extractionLog, wrapper := filepath.Join(root, "extraction-events.fifo"), filepath.Join(root, "intro-ffmpeg")
	// Production extraction forbids regular-file writes with RLIMIT_FSIZE=0.
	// An owned FIFO reports invocation events without relaxing that protection
	// or adding an untracked reader goroutine. Reads below are nonblocking.
	if err := syscall.Mkfifo(extractionLog, 0600); err != nil {
		t.Fatal(err)
	}
	extractionFD, err := syscall.Open(extractionLog, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := syscall.Close(extractionFD); err != nil {
			t.Errorf("close extraction event pipe: %v", err)
		}
	})
	// The forwarding wrapper records only the existence of a real raw muxer
	// invocation. Its stdout is the unmodified official binary's fingerprint.
	script := "#!/bin/sh\ncase \" $* \" in *\" -fp_format raw \"*) printf 'extract\\n' >> " + audioHTTPShellQuote(extractionLog) + ";; esac\nexec " + audioHTTPShellQuote(introFFmpeg) + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	f.cfg.MediaRoots, f.cfg.FFmpegPath, f.cfg.FFprobePath = []string{root}, ffmpeg, ffprobe
	f.cfg.MediaAnalysis = config.MediaAnalysisConfig{Enabled: true, CacheDirectory: filepath.Join(root, "analysis-cache"),
		CacheMaxBytes: 32 << 20, CacheMaxEntries: 8, MaxEntryBytes: 8 << 20, MaxFileBytes: 1 << 20, IntroFFmpegPath: wrapper}
	app, err := New(f.ctx, f.cfg, f.pool, f.users, f.log, "intro-skipper-integration")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := app.Close(ctx); err != nil {
			t.Errorf("close native intro integration server: %v", err)
		}
	})
	f.app, f.handler = app, app.Handler()
	if !app.mediaAnalysis.Available(library.TaskIntroAnalysisKey) {
		t.Fatalf("native matcher unavailable: %+v", app.mediaAnalysis.Status())
	}
	if err := app.taskManager.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	cookie, csrf := f.adminLogin(t)
	actor, err := f.users.Resolve(f.ctx, cookie.Value, "admin")
	if err != nil {
		t.Fatal(err)
	}
	fixture := introSkipperServerFixture{analysisProjectionFixture: analysisProjectionFixture{serverFixture: f, root: root, paths: paths,
		cookie: cookie, csrf: csrf, actor: actor, token: stringValue(t, f.embyLogin(t, "Administrator", "administrator-password"), "AccessToken")}, extractionFD: extractionFD, extractions: new(int)}
	fixture.libraryID = createAndScanAPILibrary(t, f, cookie, csrf, filepath.Join(root, "tv"), "tvshows")
	for _, path := range paths {
		var id string
		if err := f.pool.QueryRow(f.ctx, `SELECT id FROM items WHERE path=$1 AND type='Episode'`, path).Scan(&id); err != nil {
			t.Fatal(err)
		}
		fixture.ids = append(fixture.ids, id)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE libraries SET options=jsonb_set(options,'{EnableIntroDetection}','true'::jsonb) WHERE id=$1`, fixture.libraryID); err != nil {
		t.Fatal(err)
	}
	fixture.executor = &introSkipperIntegrationExecutor{inner: mediaAnalysisTaskExecutor{runtime: app.mediaAnalysis, key: library.TaskIntroAnalysisKey},
		gates: make(chan chan struct{}, 1), started: make(chan introSkipperExecutionStart, 1), completed: make(chan error, 1)}
	registry, err := tasks.NewExecutorRegistry(tasks.ExecutorRegistration{Key: library.TaskIntroAnalysisKey, Name: "Native intro integration", Executor: fixture.executor,
		AnalysisAdmission: fixture.executor.inner.admission})
	if err != nil {
		t.Fatal(err)
	}
	fixture.tasks, err = tasks.New(f.pool, app.library, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.tasks.Reconcile(f.ctx); err != nil {
		t.Fatal(err)
	}
	definition, err := fixture.tasks.GetByKey(f.ctx, library.TaskIntroAnalysisKey)
	if err != nil {
		t.Fatal(err)
	}
	// Keep this test's explicit runs independent of configuration-triggered
	// scheduling. Production schedule behavior has its own task integration suite.
	fixture.definition, err = fixture.tasks.ReplaceTriggers(f.ctx, tasks.Actor{Principal: actor, Audience: identity.AdministratorNative},
		tasks.ReplaceTriggersRequest{TaskID: definition.ID, Revision: definition.Revision, ScheduleTimezone: definition.ScheduleTimezone, Triggers: []tasks.ScheduleRule{}})
	if err != nil {
		t.Fatal(err)
	}
	app.taskStore = fixture.tasks
	app.taskManager, err = tasks.NewManager(fixture.tasks, app.library, tasks.ManagerOptions{Logger: f.log})
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (f introSkipperServerFixture) start(t *testing.T, gate chan struct{}, force bool) introSkipperExecutionStart {
	t.Helper()
	f.executor.gates <- gate
	admission, err := f.app.taskManager.Start(f.ctx, tasks.Actor{Principal: f.actor, Audience: identity.AdministratorNative}, tasks.StartRequest{
		TaskID: f.definition.ID, AnalysisInput: &library.AnalysisSelection{LibraryIDs: []string{f.libraryID}, Force: force}})
	if err != nil {
		t.Fatal(err)
	}
	if admission.Run.TotalChildren != 1 {
		t.Fatal("the two-episode fixture did not admit exactly one production child")
	}
	select {
	case started := <-f.executor.started:
		if started.Task.RunID != admission.Run.ID || len(started.Work.Sources) != 2 {
			t.Fatal("manager dispatched a different admitted cohort")
		}
		return started
	case <-time.After(20 * time.Second):
		t.Fatal("production native executor did not receive sealed work")
	case <-f.ctx.Done():
		t.Fatal(f.ctx.Err())
	}
	return introSkipperExecutionStart{}
}

func (f introSkipperServerFixture) finish(t *testing.T, started introSkipperExecutionStart, want tasks.RunState) error {
	t.Helper()
	var executionError error
	select {
	case executionError = <-f.executor.completed:
	case <-time.After(30 * time.Second):
		t.Fatal("production native executor did not return")
	case <-f.ctx.Done():
		t.Fatal(f.ctx.Err())
	}
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		run, err := f.tasks.GetRun(f.ctx, started.Task.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if !run.State.Active() {
			if run.State != want {
				t.Fatalf("native run ended as %s, want %s; executor error: %v", run.State, want, executionError)
			}
			if want == tasks.RunCompleted && (run.Scanned != 2 || run.Updated != 2 || executionError != nil) {
				t.Fatalf("native task progress or completion is incomplete: %+v %v", run, executionError)
			}
			return executionError
		}
		select {
		case <-deadline.C:
			t.Fatal("native run did not persist its terminal receipt")
		case <-f.ctx.Done():
			t.Fatal(f.ctx.Err())
		case <-ticker.C:
		}
	}
}

func (f introSkipperServerFixture) extractionCount(t *testing.T) int {
	t.Helper()
	var data [1024]byte
	for {
		n, err := syscall.Read(f.extractionFD, data[:])
		if n > 0 {
			*f.extractions += strings.Count(string(data[:n]), "\n")
		}
		if errors.Is(err, syscall.EAGAIN) || n == 0 && err == nil {
			return *f.extractions
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

func introSkipperOpenGate() chan struct{} { gate := make(chan struct{}); close(gate); return gate }

func TestAnalysisIntroSkipperProductionExecutorCachePublicationAndConfigurationFence(t *testing.T) {
	f := newIntroSkipperServerFixture(t)
	cold := f.start(t, introSkipperOpenGate(), false)
	f.finish(t, cold, tasks.RunCompleted)
	if f.extractionCount(t) != 2 {
		t.Fatal("the cold production run did not invoke the real muxer once per source")
	}
	var targetCandidate *introskipper.Candidate
	var cachedFingerprints [][]uint32
	for _, source := range cold.Work.Sources {
		var payload []byte
		if err := f.pool.QueryRow(f.ctx, `SELECT payload FROM analysis_feature_cache WHERE item_id=$1 AND profile_fingerprint=$2`, source.ItemID, cold.Work.ConfigurationFingerprint).Scan(&payload); err != nil {
			t.Fatal(err)
		}
		if len(payload) < 72 || string(payload[:4]) != "GAFB" || binary.LittleEndian.Uint16(payload[4:6]) != 4 {
			t.Fatal("production extraction did not persist GAFB4 raw fingerprints")
		}
		cached, err := library.DecodeAnalysisFeatures(payload, source.DurationTicks)
		if err != nil || len(cached.RawFingerprint) == 0 || len(cached.Audio) != 0 || len(cached.Visual) != 0 || len(cached.Refinement) != 0 || cached.FingerprintEndSeconds != introskipper.FingerprintEndSeconds(source.DurationTicks, cold.Work.Profile.IntroSkipper) {
			t.Fatalf("production raw cache lost its sequence/window or acquired visual evidence: %+v %v", cached, err)
		}
		cachedFingerprints = append(cachedFingerprints, cached.RawFingerprint)
		item, err := f.app.library.GetAnalysisItem(f.ctx, f.actor, source.ItemID)
		if err != nil || item.Detection.Status != introskipper.Qualified || item.Detection.Candidate != nil || item.Detection.IntroSkipperCandidate == nil || item.Detection.Effective == nil {
			t.Fatalf("production publication did not expose native qualified evidence: %+v %v", item.Detection, err)
		}
		candidate := item.Detection.IntroSkipperCandidate
		if candidate.UpstreamCommit != introskipper.UpstreamCommit || len(candidate.Support) != 2 || candidate.Interval.StartTicks != 0 || candidate.Interval.EndTicks < 15*media.TicksPerSecond {
			t.Fatalf("native candidate lost its upstream rule or actual pair: %+v", candidate)
		}
		if source.ItemID == f.ids[0] {
			targetCandidate = candidate
		}
	}
	if len(cachedFingerprints) != 2 || !reflect.DeepEqual(cachedFingerprints[0], cachedFingerprints[1]) || targetCandidate == nil {
		t.Fatal("the generated common audio did not retain equal raw sequences and a real target candidate")
	}
	wantMarkers := map[string]int64{"IntroStart": targetCandidate.Interval.StartTicks, "IntroEnd": targetCandidate.Interval.EndTicks}
	f.assertHTTPMarkers(t, wantMarkers)

	warm := f.start(t, introSkipperOpenGate(), false)
	f.finish(t, warm, tasks.RunCompleted)
	if warm.Work.ConfigurationFingerprint != cold.Work.ConfigurationFingerprint || f.extractionCount(t) != 2 {
		t.Fatal("the same admitted native profile did not reuse its complete raw cache")
	}
	f.assertHTTPMarkers(t, wantMarkers)

	// A real administrator decision remains effective across a forced native
	// rerun; no automatic publication can override the rejection tombstone.
	item, err := f.app.library.GetAnalysisItem(f.ctx, f.actor, f.ids[0])
	if err != nil {
		t.Fatal(err)
	}
	detection, err := f.app.library.DecideAnalysisIntro(f.ctx, f.actor, f.ids[0], library.AnalysisDecision{Revision: item.Detection.Revision,
		SourceRevision: item.Detection.SourceRevision, ManualRevision: item.Detection.ManualRevision, Action: "reject"})
	if err != nil || !detection.Suppressed {
		t.Fatalf("native candidate rejection failed: %+v %v", detection, err)
	}
	f.assertHTTPMarkers(t, map[string]int64{})
	rejected := f.start(t, introSkipperOpenGate(), true)
	f.finish(t, rejected, tasks.RunCompleted)
	f.assertHTTPMarkers(t, map[string]int64{})
	if f.extractionCount(t) != 4 {
		t.Fatal("forced work did not refresh fingerprints independently of rejection")
	}

	blockedGate := make(chan struct{})
	blocked := f.start(t, blockedGate, false)
	if _, found, err := f.app.library.GetAnalysisFeatures(f.ctx, blocked.Task.ChildID, blocked.Work.Sources[0].ItemID, blocked.Task.Fence); err != nil || !found {
		t.Fatalf("live admitted work could not read its original native cache: %t %v", found, err)
	}
	configuration, err := f.app.library.GetAnalysisConfiguration(f.ctx, f.actor)
	if err != nil {
		t.Fatal(err)
	}
	configuration.Profile.IntroSkipper.MinimumIntroDuration++
	saved, err := f.app.library.UpdateAnalysisConfiguration(f.ctx, f.actor, library.AnalysisConfigurationUpdate{Revision: configuration.Revision, Profile: configuration.Profile})
	if err != nil || saved.Revision == configuration.Revision {
		t.Fatalf("native parameter change did not advance configuration: %+v %v", saved, err)
	}
	if _, found, err := f.app.library.GetAnalysisFeatures(f.ctx, blocked.Task.ChildID, blocked.Work.Sources[0].ItemID, blocked.Task.Fence); err == nil || found {
		t.Fatal("old sealed work retained feature authority after its configured algorithm changed")
	}
	var caches int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM analysis_feature_cache`).Scan(&caches); err != nil || caches != 0 {
		t.Fatal("configuration change retained stale raw fingerprints")
	}
	close(blockedGate)
	if err := f.finish(t, blocked, tasks.RunFailed); err == nil {
		t.Fatal("obsolete admitted work completed after its configuration changed")
	}
	if f.extractionCount(t) != 4 {
		t.Fatal("obsolete work decoded media after its configuration was invalidated")
	}
	f.assertHTTPMarkers(t, map[string]int64{})

	fresh := f.start(t, introSkipperOpenGate(), false)
	f.finish(t, fresh, tasks.RunCompleted)
	if fresh.Work.ConfigurationFingerprint == cold.Work.ConfigurationFingerprint || fresh.Work.Execution.IntroSkipperOptions != saved.Profile.IntroSkipper || f.extractionCount(t) != 6 {
		t.Fatal("new work failed to bind the configured options and perform fresh extraction")
	}
	f.assertHTTPMarkers(t, map[string]int64{})
	f.manual(t, false)
	f.assertHTTPMarkers(t, map[string]int64{"IntroStart": 7 * media.TicksPerSecond, "IntroEnd": 17 * media.TicksPerSecond})
	f.manual(t, true)
	f.assertHTTPMarkers(t, map[string]int64{})
}
