//go:build linux

package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/introskipper"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
)

type creditsCacheFixtureOptions struct {
	SourceCount int
	Prober      library.Prober
	WriteSource func(path string) error
	ToolPath    string
	DirectTool  bool
	Output      string
}

type creditsCacheStarted struct {
	ctx  context.Context
	task tasks.Work
	work library.AnalysisWork
	gate chan struct{}
}

type creditsCacheOutcome struct {
	hashes  map[string]string
	elapsed time.Duration
	err     error
}

type creditsCacheExecutor struct {
	runtime *mediaAnalysisRuntime
	started chan creditsCacheStarted
	done    chan creditsCacheOutcome
}

func (*creditsCacheExecutor) Available() bool { return true }

// Only the production fingerprint stage runs here. Admission, operation roots,
// child capabilities, source reads, tool proofs and retirement remain real.
func (e *creditsCacheExecutor) Execute(ctx context.Context, task tasks.Work, progress func(tasks.Progress) error) (resultErr error) {
	outcome := creditsCacheOutcome{hashes: make(map[string]string)}
	defer func() { outcome.err = resultErr; e.done <- outcome }()
	ctx, closeOperation, err := e.runtime.server.library.BeginAnalysisOperation(ctx, task.ChildID, task.Fence)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, closeOperation()) }()
	task = task.WithContext(ctx)
	work, err := e.runtime.server.library.GetAnalysisWork(ctx, task.ChildID, task.Fence)
	if err != nil {
		return err
	}
	gate := make(chan struct{})
	select {
	case e.started <- creditsCacheStarted{ctx, task, work, gate}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-gate:
	case <-ctx.Done():
		return ctx.Err()
	}
	started := time.Now()
	defer func() { outcome.elapsed = time.Since(started) }()
	for index, source := range work.Sources {
		hash, _, err := e.runtime.creditsSourceFingerprint(ctx, task, work, source)
		if err != nil {
			return err
		}
		outcome.hashes[source.ItemID] = hash
		if err := progress(tasks.Progress{Processed: int64(index + 1)}); err != nil {
			return err
		}
	}
	return nil
}

type creditsCacheFixture struct {
	analysisProjectionFixture
	executor   *creditsCacheExecutor
	definition tasks.Definition
	tool       string
	eventFD    int
	probes     int
	extracts   int
}

func newCreditsCacheFixture(t *testing.T, options creditsCacheFixtureOptions) *creditsCacheFixture {
	t.Helper()
	f := newServerFixtureWithTimeout(t, 10*time.Minute)
	closeFixtureCatalogForReplacement(t, f)
	root := t.TempDir()
	if options.Prober == nil {
		options.Prober = analysisProjectionProber{}
	}
	catalog, err := library.New(f.pool, options.Prober, []string{root})
	if err != nil {
		t.Fatal(err)
	}
	installFixtureCatalog(t, f, catalog)
	f.app.cfg.MediaRoots, f.cfg.MediaRoots = []string{root}, []string{root}
	f.handler = f.app.Handler()
	f.bootstrap(t)
	cookie, csrf := f.adminLogin(t)
	fixture := &creditsCacheFixture{analysisProjectionFixture: analysisProjectionFixture{serverFixture: f, root: root, cookie: cookie, csrf: csrf}}
	fixture.actor, err = f.users.Resolve(f.ctx, cookie.Value, "admin")
	if err != nil {
		t.Fatal(err)
	}
	season := filepath.Join(root, "tv", "Credits Cache Show", "Season 01")
	if err := os.MkdirAll(season, 0700); err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= options.SourceCount; index++ {
		path := filepath.Join(season, fmt.Sprintf("Credits.Cache.Show.S01E%02d.mkv", index))
		if options.WriteSource != nil {
			err = options.WriteSource(path)
		} else {
			err = os.WriteFile(path, []byte(strings.Repeat(fmt.Sprintf("episode-%02d-complete-content;", index), 12000)), 0600)
		}
		if err != nil {
			t.Fatal(err)
		}
		fixture.paths = append(fixture.paths, path)
	}
	fixture.libraryID = createAndScanAPILibrary(t, f, cookie, csrf, filepath.Join(root, "tv"), "tvshows")
	for _, path := range fixture.paths {
		var id string
		if err := f.pool.QueryRow(f.ctx, `SELECT id FROM items WHERE path=$1 AND type='Episode'`, path).Scan(&id); err != nil {
			t.Fatal(err)
		}
		fixture.ids = append(fixture.ids, id)
	}
	if err := f.app.taskManager.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE libraries SET options=options||'{"EnableCreditsDetection":true}'::jsonb WHERE id=$1`, fixture.libraryID); err != nil {
		t.Fatal(err)
	}
	// RLIMIT_FSIZE=0 prohibits regular-file logging in production children.
	// This owned, nonblocking FIFO needs no reader goroutine or weaker limits.
	fifo := filepath.Join(root, "credits-events.fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	fixture.eventFD, err = syscall.Open(fifo, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(fixture.eventFD) })
	output := options.Output
	if output == "" {
		output = `printf '\001\000\000\000\377\377\377\377'`
	}
	script := "#!/bin/sh\ncase \" $* \" in *\" -fp_format raw \"*) printf x >> " + audioHTTPShellQuote(fifo) + ";; *) printf p >> " + audioHTTPShellQuote(fifo) + ";; esac\n"
	if options.ToolPath != "" {
		script += "exec " + audioHTTPShellQuote(options.ToolPath) + " \"$@\"\n"
	} else {
		script += "case \"$1\" in\n-version) printf 'ffmpeg version fixture\\n';;\n-hide_banner)\nif [ \"$2\" = '-h' ]; then\ncat <<'MUXER'\n" +
			"Muxer chromaprint [Chromaprint]:\n    Default audio codec: pcm_s16le.\nchromaprint muxer AVOptions:\n" +
			"  -silence_threshold <int> E.......... threshold (from -1 to 32767) (default -1)\n  -algorithm <int> E.......... algorithm (from 0 to INT_MAX) (default 1)\n" +
			"  -fp_format <int> E.......... format (from 0 to 2) (default base64)\n     raw 0 E.......... binary raw fingerprint\nMUXER\n" +
			"elif [ \"$2\" = '-encoders' ]; then\nprintf ' A....D pcm_s16le PCM signed 16-bit little-endian\\n'\nelse\n" + output + "\nfi;;\nesac\n"
	}
	fixture.tool = filepath.Join(root, "credits-ffmpeg")
	toolBytes := []byte(script)
	if options.DirectTool {
		// Timing must include the real executable's digest on every attempt.
		// This mode deliberately bypasses the counting wrapper entirely.
		if !filepath.IsAbs(options.ToolPath) {
			t.Fatal("direct tool measurement requires an absolute executable path")
		}
		fixture.tool, err = filepath.EvalSymlinks(options.ToolPath)
		if err == nil {
			toolBytes, err = os.ReadFile(fixture.tool)
		}
		if err != nil {
			t.Fatal(err)
		}
	} else {
		if err := os.WriteFile(fixture.tool, toolBytes, 0700); err != nil {
			t.Fatal(err)
		}
	}
	digest := sha256.Sum256(toolBytes)
	toolHash := hex.EncodeToString(digest[:])
	audioProfile, err := media.CreditsSkipperAlgorithmProfile(media.AnalysisAvailability{IntroSkipperAvailable: true, IntroFFmpegSHA256: toolHash})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &mediaAnalysisRuntime{server: f.app, creditsAudioProfile: audioProfile,
		extractor: media.AnalysisExtractor{IntroFFmpegPath: fixture.tool, ExpectedIntroFFmpegSHA256: toolHash}}
	fixture.executor = &creditsCacheExecutor{runtime: runtime, started: make(chan creditsCacheStarted, 2), done: make(chan creditsCacheOutcome, 2)}
	execution := library.AnalysisExecutionProfile{Version: library.AnalysisExecutionProfileVersion, Available: true,
		FFmpegSHA256: toolHash, FFprobeSHA256: toolHash, FingerprintSHA256: toolHash, DetectorVersion: introskipper.CreditsVersion,
		IntroSkipperOptions: introskipper.DefaultOptions(), IntroProfile: "credits-cache-integration-v1;" + toolHash}
	registry, err := tasks.NewExecutorRegistry(tasks.ExecutorRegistration{Key: library.TaskCreditsAnalysisKey, Name: "Credits cache fingerprint stage", Executor: fixture.executor,
		AnalysisAdmission: func(tx library.OwnedTx, request tasks.AnalysisAdmissionRequest) (tasks.AnalysisAdmissionBinding, error) {
			binding, err := library.PrepareAnalysis(tx, request.TaskKey, request.Selection, execution)
			return tasks.AnalysisAdmissionBinding{ConfigurationFingerprint: binding.ConfigurationFingerprint, Bind: binding.Bind, SnapshotChildren: binding.SnapshotChildren}, err
		}})
	if err != nil {
		t.Fatal(err)
	}
	fixture.tasks, err = tasks.New(f.pool, f.app.library, registry)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.tasks.Reconcile(f.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE task_definitions SET enabled=true,revision=revision+1 WHERE key=$1`, library.TaskCreditsAnalysisKey); err != nil {
		t.Fatal(err)
	}
	definition, err := fixture.tasks.GetByKey(f.ctx, library.TaskCreditsAnalysisKey)
	if err != nil {
		t.Fatal(err)
	}
	fixture.definition, err = fixture.tasks.ReplaceTriggers(f.ctx, tasks.Actor{Principal: fixture.actor, Audience: identity.AdministratorNative},
		tasks.ReplaceTriggersRequest{TaskID: definition.ID, Revision: definition.Revision, ScheduleTimezone: definition.ScheduleTimezone, Triggers: []tasks.ScheduleRule{}})
	if err != nil {
		t.Fatal(err)
	}
	f.app.taskStore = fixture.tasks
	f.app.taskManager, err = tasks.NewManager(fixture.tasks, f.app.library, tasks.ManagerOptions{Logger: f.log, MaxConcurrent: func() int { return 1 }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := f.app.taskManager.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return fixture
}

func (f *creditsCacheFixture) start(t *testing.T, force bool) tasks.Admission {
	t.Helper()
	admission, err := f.app.taskManager.Start(f.ctx, tasks.Actor{Principal: f.actor, Audience: identity.AdministratorNative},
		tasks.StartRequest{TaskID: f.definition.ID, AnalysisInput: &library.AnalysisSelection{LibraryIDs: []string{f.libraryID}, Force: force}})
	if err != nil {
		t.Fatal(err)
	}
	return admission
}

func (f *creditsCacheFixture) next(t *testing.T) creditsCacheStarted {
	t.Helper()
	select {
	case started := <-f.executor.started:
		return started
	case <-f.ctx.Done():
		t.Fatal(f.ctx.Err())
	case <-time.After(30 * time.Second):
		t.Fatal("manager did not dispatch the next sealed credits child")
	}
	return creditsCacheStarted{}
}

func (f *creditsCacheFixture) finish(t *testing.T, started creditsCacheStarted) creditsCacheOutcome {
	t.Helper()
	close(started.gate)
	select {
	case outcome := <-f.executor.done:
		return outcome
	case <-f.ctx.Done():
		t.Fatal(f.ctx.Err())
	}
	return creditsCacheOutcome{}
}

func (f *creditsCacheFixture) waitRun(t *testing.T, runID string, want tasks.RunState) {
	t.Helper()
	deadline, ticker := time.NewTimer(15*time.Second), time.NewTicker(25*time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
	for {
		run, err := f.tasks.GetRun(f.ctx, runID)
		if err != nil {
			t.Fatal(err)
		}
		if !run.State.Active() {
			if run.State != want {
				t.Fatalf("credits run state = %s, want %s", run.State, want)
			}
			return
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("credits run did not retire")
		case <-f.ctx.Done():
			t.Fatal(f.ctx.Err())
		}
	}
}

func (f *creditsCacheFixture) counts(t *testing.T) (probes, extracts int) {
	t.Helper()
	var buffer [4096]byte
	for {
		n, err := syscall.Read(f.eventFD, buffer[:])
		if n > 0 {
			f.probes += strings.Count(string(buffer[:n]), "p")
			f.extracts += strings.Count(string(buffer[:n]), "x")
		}
		if errors.Is(err, syscall.EAGAIN) || n == 0 && err == nil {
			return f.probes, f.extracts
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestCreditsFingerprintCacheReusesOnlyWithinOneNonForceRun(t *testing.T) {
	f := newCreditsCacheFixture(t, creditsCacheFixtureOptions{SourceCount: 24})
	wantHashes := make(map[string]string)
	for index, path := range f.paths {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(content)
		wantHashes[f.ids[index]] = hex.EncodeToString(digest[:])
	}
	for runIndex, force := range []bool{false, false, true} {
		admission := f.start(t, force)
		if admission.Run.TotalChildren != 2 {
			t.Fatalf("24 episodes admitted %d children, want two overlapping cohorts", admission.Run.TotalChildren)
		}
		var first creditsCacheStarted
		for child := 0; child < 2; child++ {
			started := f.next(t)
			if len(started.work.Sources) != 24 || started.task.RunID != admission.Run.ID {
				t.Fatal("manager did not retain all 24 sources in each child")
			}
			if child == 0 {
				first = started
			} else {
				crossChild := started.task
				crossChild.ChildID = first.task.ChildID
				for _, forged := range []tasks.Work{{RunID: started.task.RunID, ChildID: started.task.ChildID}, crossChild, first.task} {
					if _, raw, err := f.executor.runtime.creditsSourceFingerprint(started.ctx, forged, started.work, started.work.Sources[0]); err == nil || len(raw) != 0 {
						t.Fatal("cached bytes transferred authority to a reconstructed or different child")
					}
				}
			}
			if force && child == 0 {
				// Prepopulate this exact Force run so a miss caused only by its
				// new RunID cannot accidentally satisfy the bypass assertion.
				source := started.work.Sources[0]
				key, ok := creditsFingerprintKey(started.work, source, wantHashes[source.ItemID], f.executor.runtime.creditsAudioProfile, 5)
				if !ok {
					t.Fatal("fixture could not construct the admitted Force cache key")
				}
				f.executor.runtime.creditsFingerprints.put(key, []uint32{99})
			}
			cacheEntries := f.executor.runtime.creditsFingerprints.count
			outcome := f.finish(t, started)
			if outcome.err != nil || !reflect.DeepEqual(outcome.hashes, wantHashes) {
				t.Fatalf("child did not digest every complete source: %v; hashes=%d", outcome.err, len(outcome.hashes))
			}
			if force && f.executor.runtime.creditsFingerprints.count != cacheEntries {
				t.Fatal("Force wrote reusable fingerprint entries")
			}
			wantExtracts := 24 * (runIndex + 1)
			if force {
				wantExtracts = 48 + 24*(child+1)
			}
			if probes, extracts := f.counts(t); probes != (runIndex*2+child+1)*24*3 || extracts != wantExtracts {
				t.Fatalf("run %d child %d: probes=%d extracts=%d, want %d/%d", runIndex, child, probes, extracts, (runIndex*2+child+1)*24*3, wantExtracts)
			}
		}
		f.waitRun(t, admission.Run.ID, tasks.RunCompleted)
	}
}

func TestCreditsFingerprintCacheCannotHideChangedAuthoritySourceOrTool(t *testing.T) {
	for _, change := range []string{"configuration", "source", "tool"} {
		t.Run(change, func(t *testing.T) {
			f := newCreditsCacheFixture(t, creditsCacheFixtureOptions{SourceCount: 24})
			admission := f.start(t, false)
			if outcome := f.finish(t, f.next(t)); outcome.err != nil {
				t.Fatal(outcome.err)
			}
			second := f.next(t)
			switch change {
			case "configuration":
				configuration, err := f.app.library.GetAnalysisConfiguration(f.ctx, f.actor)
				if err != nil {
					t.Fatal(err)
				}
				configuration.Profile.IntroSkipper.MinimumIntroDuration++
				if _, err := f.app.library.UpdateAnalysisConfiguration(f.ctx, f.actor, library.AnalysisConfigurationUpdate{Revision: configuration.Revision, Profile: configuration.Profile}); err != nil {
					t.Fatal(err)
				}
			case "source":
				if err := os.WriteFile(f.paths[0], []byte("changed source"), 0600); err != nil {
					t.Fatal(err)
				}
			case "tool":
				if err := os.WriteFile(f.tool, []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if outcome := f.finish(t, second); outcome.err == nil {
				t.Fatal("warm child accepted invalidated configuration, source or tool")
			}
			f.waitRun(t, admission.Run.ID, tasks.RunFailed)
			if _, extracts := f.counts(t); extracts != 24 {
				t.Fatalf("invalidated warm child performed fresh extraction: %d", extracts)
			}
		})
	}
}

func TestCreditsFingerprintCacheDoesNotRetainEmptyOrFailedExtraction(t *testing.T) {
	for _, output := range []string{":", "printf '\\001\\000\\000\\000'; exit 1"} {
		t.Run(output, func(t *testing.T) {
			f := newCreditsCacheFixture(t, creditsCacheFixtureOptions{SourceCount: 2, Output: output})
			f.start(t, false)
			started := f.next(t)
			for attempt := 0; attempt < 2; attempt++ {
				_, raw, err := f.executor.runtime.creditsSourceFingerprint(started.ctx, started.task, started.work, started.work.Sources[0])
				if len(raw) != 0 || (err == nil) != (output == ":") {
					t.Fatalf("empty/failed extraction changed its result: %v %v", raw, err)
				}
			}
			if probes, extracts := f.counts(t); probes != 6 || extracts != 2 {
				t.Fatalf("empty/failed result was reused: probes=%d extracts=%d", probes, extracts)
			}
			if f.executor.runtime.creditsFingerprints.count != 0 {
				t.Fatal("empty/failed extraction published a cache entry")
			}
			// Fixture cleanup cancels and joins this gated worker before retiring
			// the catalog or FIFO; it cannot start a third extraction afterward.
		})
	}
}
