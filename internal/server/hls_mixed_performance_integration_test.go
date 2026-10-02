//go:build linux

package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
)

type hlsMixedProfileTracer struct {
	priority                        hlsPriorityProfileTracer
	enabled                         atomic.Bool
	backgroundSQL, backgroundBegins atomic.Int64
	cachedCheckpoints               atomic.Int64
	controlOverlap                  atomic.Int64
}

func (trace *hlsMixedProfileTracer) TraceQueryStart(ctx context.Context, connection *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	trace.priority.TraceQueryStart(ctx, connection, data)
	if trace.enabled.Load() && ctx.Value(hlsPriorityProfileContextKey{}) == nil {
		trace.backgroundSQL.Add(1)
		statement := strings.ToLower(strings.TrimSpace(data.SQL))
		if strings.HasPrefix(statement, "begin") {
			trace.backgroundBegins.Add(1)
		}
		if strings.HasPrefix(statement, "with progress_run as materialized") {
			trace.cachedCheckpoints.Add(1)
		}
	}
	return ctx
}

func (*hlsMixedProfileTracer) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (trace *hlsMixedProfileTracer) wrap(next http.Handler) http.Handler {
	marked := trace.priority.wrap(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if current := trace.priority.current.Load(); current != nil && r.Header.Get(hlsPriorityProfileHeader) == "stopped" {
			// B's stop competes with A's cached readers. The accepted priority
			// wrapper observes same-play stop_get work, so capture this separately.
			trace.controlOverlap.Store(current.steady.active.Load())
		}
		marked.ServeHTTP(w, r)
	})
}

type hlsMixedProfileRun struct {
	run tasks.Run
	job library.Job
}

// Observe through the independent fixture pool, leaving the measured request
// pool free of instrumentation queries. One statement gives a consistent MVCC
// snapshot of the actual coordinator, child and scanner relation.
func hlsMixedProfileReadRun(ctx context.Context, observer *pgxpool.Pool, runID, libraryID string) (hlsMixedProfileRun, bool, error) {
	var record hlsMixedProfileRun
	var child tasks.Child
	var children int64
	record.run.ID = runID
	err := observer.QueryRow(ctx, `SELECT r.state,r.scanned,r.added,r.updated,r.total_children,r.started_at,r.finished_at,
		COALESCE(c.id,''),COALESCE(c.library_id,''),COALESCE(c.state,''),COALESCE(c.scanned,0),COALESCE(c.added,0),COALESCE(c.updated,0),
		COALESCE(j.id,''),COALESCE(j.task_child_id,''),COALESCE(j.library_id,''),COALESCE(j.status,''),COALESCE(j.scanned,0),
		COALESCE(j.added,0),COALESCE(j.updated,0),COALESCE(j.force_probe,false),COALESCE(j.cancel_requested,false),j.started_at,j.finished_at,
		(SELECT count(*) FROM task_run_children counter WHERE counter.run_id=r.id)
		FROM task_runs r LEFT JOIN task_run_children c ON c.run_id=r.id AND c.library_id=$2
		LEFT JOIN scan_jobs j ON j.id=c.scan_job_id WHERE r.id=$1`, runID, libraryID).
		Scan(&record.run.State, &record.run.Scanned, &record.run.Added, &record.run.Updated, &record.run.TotalChildren, &record.run.StartedAt, &record.run.FinishedAt,
			&child.ID, &child.LibraryID, &child.State, &child.Scanned, &child.Added, &child.Updated,
			&record.job.ID, &record.job.TaskChildID, &record.job.LibraryID, &record.job.Status, &record.job.Scanned,
			&record.job.Added, &record.job.Updated, &record.job.ForceProbe, &record.job.CancelRequested, &record.job.StartedAt, &record.job.FinishedAt, &children)
	if err != nil {
		return record, false, err
	}
	if children != 1 || record.run.TotalChildren != 1 || child.ID == "" || child.LibraryID != libraryID {
		return record, false, fmt.Errorf("mixed task does not have one exact library child")
	}
	job := record.job
	if job.ID != "" && (job.TaskChildID != child.ID || job.LibraryID != libraryID || job.ForceProbe) {
		return record, false, fmt.Errorf("mixed scan lost its task ownership or ordinary scan mode")
	}
	if !record.run.State.Active() {
		if record.run.FinishedAt == nil || child.State.Active() ||
			record.run.Scanned != int64(job.Scanned) || record.run.Added != int64(job.Added) || record.run.Updated != int64(job.Updated) ||
			child.Scanned != int64(job.Scanned) || child.Added != int64(job.Added) || child.Updated != int64(job.Updated) ||
			(record.run.State != tasks.RunCompleted && record.run.State != tasks.RunCancelled) {
			return record, false, fmt.Errorf("mixed task terminal snapshot: run=%s job=%s", record.run.State, job.Status)
		}
		if job.ID == "" {
			if record.run.State != tasks.RunCancelled || child.State != tasks.ChildCancelled || job.Scanned != 0 || job.Added != 0 || job.Updated != 0 {
				return record, false, fmt.Errorf("unadmitted mixed task did not settle inertly")
			}
			record.job.Status = "NotAdmitted"
		} else if job.FinishedAt == nil || job.Status == "Completed" && child.State != tasks.ChildCompleted ||
			job.Status == "Cancelled" && child.State != tasks.ChildCancelled || job.Status != "Completed" && job.Status != "Cancelled" {
			return record, false, fmt.Errorf("mixed scanner and child terminal states disagree")
		}
	}
	running := job.Status == "Running" && job.Scanned > 0 && child.State == tasks.ChildRunning && !job.CancelRequested
	return record, running, nil
}

type hlsMixedProfileScans struct {
	runs []hlsMixedProfileRun
	err  error
}

type hlsMixedProfileController struct {
	stop      chan struct{}
	ready     chan struct{}
	done      chan struct{}
	stopOnce  sync.Once
	readyOnce sync.Once
	result    hlsMixedProfileScans
}

func (controller *hlsMixedProfileController) finish() {
	controller.stopOnce.Do(func() { close(controller.stop) })
	<-controller.done
}

// The scanner is real task-owned work. Each run has exactly one library child;
// there are at most eight serial runs, and no prober or database delay is added.
func hlsMixedProfileStartScans(ctx context.Context, app *Server, observer *pgxpool.Pool, actor tasks.Actor, taskID, libraryID, label string) *hlsMixedProfileController {
	controller := &hlsMixedProfileController{stop: make(chan struct{}), ready: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(controller.done)
		var admitted []string
		collected := make(map[string]bool)
		// Join the admission loop before its done signal. Every committed run
		// is settled even when the workload context or observation itself fails.
		defer func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			for _, runID := range admitted {
				if collected[runID] {
					continue
				}
				if _, err := app.taskManager.Stop(cleanup, actor, runID); err != nil {
					controller.result.err = fmt.Errorf("settle admitted mixed task: %w", err)
					return
				}
				for {
					record, _, err := hlsMixedProfileReadRun(cleanup, observer, runID, libraryID)
					if err != nil {
						controller.result.err = err
						return
					}
					if !record.run.State.Active() {
						controller.result.runs = append(controller.result.runs, record)
						break
					}
					select {
					case <-cleanup.Done():
						controller.result.err = cleanup.Err()
						return
					case <-time.After(10 * time.Millisecond):
					}
				}
			}
		}()
		for index := range 8 {
			select {
			case <-controller.stop:
				return
			default:
			}
			admission, err := app.taskManager.Start(ctx, actor, tasks.StartRequest{TaskID: taskID, RequestID: fmt.Sprintf("mixed-%s-%d", label, index)})
			if admission.Run.ID != "" {
				admitted = append(admitted, admission.Run.ID)
			}
			if err != nil || !admission.Admitted || admission.Run.TotalChildren != 1 {
				controller.result.err = fmt.Errorf("mixed task admission: admitted=%t children=%d error=%w", admission.Admitted, admission.Run.TotalChildren, err)
				return
			}
			for {
				select {
				case <-controller.stop:
					return
				default:
				}
				record, running, err := hlsMixedProfileReadRun(ctx, observer, admission.Run.ID, libraryID)
				if err != nil {
					controller.result.err = err
					return
				}
				if running {
					controller.readyOnce.Do(func() { close(controller.ready) })
				}
				if !record.run.State.Active() {
					controller.result.runs = append(controller.result.runs, record)
					collected[record.run.ID] = true
					break
				}
				select {
				case <-ctx.Done():
					controller.result.err = ctx.Err()
					return
				case <-time.After(10 * time.Millisecond):
				}
			}
		}
		select {
		case <-controller.stop:
			return
		default:
		}
		controller.result.err = fmt.Errorf("mixed task reached its eight-run cap before the HTTP load ended")
	}()
	return controller
}

func hlsMixedProfileProbeCount(t *testing.T, path string) int {
	t.Helper()
	encoded, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal("read the transparent probe invocation counter")
	}
	return strings.Count(string(encoded), "probe\n")
}

func hlsMixedProfileWarmGraph(t *testing.T, h *hlsHTTPFixture, graph hlsHTTPGraph) []byte {
	t.Helper()
	var expected []byte
	for index, child := range graph.children {
		response := h.request(t, http.MethodGet, child, nil, nil)
		expectHLSHTTPStatus(t, response, http.StatusOK)
		if index == 0 {
			expected = response.body
		}
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		var jobs, complete int
		if err := h.f.pool.QueryRow(h.f.ctx, `SELECT count(*),count(*) FILTER (WHERE state='completed') FROM encoding_jobs WHERE play_session_id=$1`, graph.playID).Scan(&jobs, &complete); err != nil {
			t.Fatal("read mixed fixture encoder completion")
		}
		if jobs > 0 && jobs == complete && len(expected) > 0 {
			return expected
		}
		if time.Now().After(deadline) {
			t.Fatal("encoding was not completed before mixed hot-cache sampling")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func hlsMixedProfileScanRunning(ctx context.Context, pool *pgxpool.Pool, libraryID string) bool {
	var running bool
	err := pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM scan_jobs j JOIN task_run_children c ON c.id=j.task_child_id AND c.scan_job_id=j.id
		WHERE j.library_id=$1 AND j.status='Running' AND c.state='running' AND NOT j.force_probe AND NOT j.cancel_requested)`, libraryID).Scan(&running)
	return err == nil && running
}

func hlsMixedProfileOverlap(job library.Job, start, end time.Time) time.Duration {
	if job.StartedAt == nil || job.FinishedAt == nil {
		return 0
	}
	if job.StartedAt.After(start) {
		start = *job.StartedAt
	}
	if job.FinishedAt.Before(end) {
		end = *job.FinishedAt
	}
	if end.After(start) {
		return end.Sub(start)
	}
	return 0
}

// Reuses the accepted priority profile's real HTTP helpers. A and B have
// independent authentication/play owners: this measures shared pool pressure,
// not contention between reads and stop writes on the same playback row. Real
// scans and cached delivery overlap without artificial locks or storage delay.
// The fixed corpus and HTTP bytes do not measure encoder/GPU playback capacity.
func TestHTTPMixedScanCachedHLSPerformance(t *testing.T) {
	if os.Getenv("GOBY_HLS_MIXED_PERFORMANCE") != "1" {
		t.Skip("GOBY_HLS_MIXED_PERFORMANCE=1 enables the mixed scan and cached HLS profile")
	}
	applications := []bool{false, true}
	applicationMode := strings.ToLower(strings.TrimSpace(os.Getenv("GOBY_MIXED_APPLICATION_MODE")))
	switch applicationMode {
	case "":
	case "normal":
		applications = []bool{false}
	case "key":
		applications = []bool{true}
	default:
		t.Fatal("GOBY_MIXED_APPLICATION_MODE must be empty, normal, or key")
	}
	concurrencies := []int{8, 32}
	callerFilter := strings.TrimSpace(os.Getenv("GOBY_MIXED_CALLERS"))
	switch callerFilter {
	case "":
	case "8":
		concurrencies = []int{8}
	case "32":
		concurrencies = []int{32}
	default:
		t.Fatal("GOBY_MIXED_CALLERS must be empty, 8, or 32")
	}
	privateRoot := os.Getenv("GOBY_MIXED_FIXTURE_ROOT")
	info, err := os.Lstat(privateRoot)
	if err != nil || !filepath.IsAbs(privateRoot) || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		t.Fatal("GOBY_MIXED_FIXTURE_ROOT must select an existing absolute private directory")
	}
	t.Setenv("TMPDIR", privateRoot)
	fixtureRoot := t.TempDir()
	t.Setenv("TMPDIR", fixtureRoot)
	h := newHLSHTTPFixture(t, 5*time.Minute)
	keys := applicationMediaIssueKeys(t, h.f, h.accounts.admin.headers.Get("X-Emby-Token"))
	// Both independent keys report the same public application metadata. Keep
	// their credential/client identities distinct without adding metadata churn.
	credentialIDs := []string{keys[0].principal.SessionID, keys[1].principal.SessionID}
	if _, err := h.f.pool.Exec(h.f.ctx, `UPDATE sessions SET client_name='Mixed HLS application' WHERE id=ANY($1::text[])`, credentialIDs); err != nil {
		t.Fatal("normalize the fixed application credential metadata")
	}
	if _, err := h.f.pool.Exec(h.f.ctx, `UPDATE application_key_clients SET client_name='Mixed HLS application' WHERE credential_id=ANY($1::text[])`, credentialIDs); err != nil {
		t.Fatal("normalize the existing independent application client metadata")
	}
	source, err := os.ReadFile(h.path)
	if err != nil {
		t.Fatal("read the original real HLS fixture before corpus replication")
	}
	const copies = 128
	for index := range copies {
		path := filepath.Join(filepath.Dir(h.path), fmt.Sprintf("Mixed.Scan.Copy.%03d.mp4", index))
		if err := os.WriteFile(path, source, 0o600); err != nil {
			t.Fatal("write the fixed real media scan corpus")
		}
	}
	countPath, wrapperPath := filepath.Join(fixtureRoot, "ffprobe.calls"), filepath.Join(fixtureRoot, "ffprobe-record.sh")
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	wrapper := "#!/bin/sh\nprintf 'probe\\n' >> " + quote(countPath) + "\nexec " + quote(h.ffprobe) + " \"$@\"\n"
	if err := os.WriteFile(wrapperPath, []byte(wrapper), 0o700); err != nil {
		t.Fatal("write the transparent real ffprobe invocation recorder")
	}
	h.server.Close()
	if err := h.f.app.Close(h.f.ctx); err != nil {
		t.Fatal("join the original server before creating the mixed profile generation")
	}
	trace := &hlsMixedProfileTracer{}
	configuration := h.f.pool.Config()
	configuration.MaxConns, configuration.ConnConfig.Tracer = 16, trace
	hlsProfileConfigureData(configuration)
	pool, err := pgxpool.NewWithConfig(h.f.ctx, configuration)
	if err != nil {
		t.Fatal("create the mixed profile request and task pool")
	}
	t.Cleanup(pool.Close)
	cfg := h.f.cfg
	cfg.FFprobePath = wrapperPath
	users := identity.New(pool)
	profileOptions := hlsProfileOptions(t, h.f.ctx, pool)
	app, err := New(h.f.ctx, cfg, pool, users, h.f.log, "mixed-scan-hls-profile", profileOptions...)
	if err != nil {
		t.Fatal("restart the real mixed profile server")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := app.Close(ctx); err != nil {
			t.Error("join the mixed profile server")
		}
	})
	h.f.app, h.f.users, h.f.handler = app, users, app.Handler()
	server := httptest.NewServer(trace.wrap(h.f.handler))
	h.server = server
	t.Cleanup(server.Close)
	actorPrincipal, err := users.ResolveEmby(h.f.ctx, h.accounts.admin.headers.Get("X-Emby-Token"))
	if err != nil {
		t.Fatal("restore the task administrator after the complete restart")
	}
	actor := tasks.Actor{Principal: actorPrincipal, Audience: identity.AdministratorEmby}
	definition, err := app.taskStore.GetByKey(h.f.ctx, tasks.LibraryScanKey)
	if err != nil {
		t.Fatal("read the real library scan definition")
	}
	// Complete one real task scan before collecting any mixed observations.
	coldStarted := time.Now()
	admission, err := app.taskManager.Start(h.f.ctx, actor, tasks.StartRequest{TaskID: definition.ID, RequestID: "mixed-corpus-prime"})
	if err != nil || !admission.Admitted {
		t.Fatal("admit the complete cold corpus scan")
	}
	for {
		run, err := app.taskStore.GetRun(h.f.ctx, admission.Run.ID)
		if err != nil {
			t.Fatal("read the cold corpus scan")
		}
		if !run.State.Active() {
			if run.State != tasks.RunCompleted || run.Scanned != copies+1 || hlsMixedProfileProbeCount(t, countPath) == 0 {
				t.Fatal("the cold scan did not really probe and index the fixed corpus")
			}
			break
		}
		if h.f.ctx.Err() != nil {
			t.Fatal("cold corpus scan exceeded the total profile deadline")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Logf("mixed_scan_corpus_setup copies=%d corpus_media=%d cold_scanned=%d cold_probe_calls=%d elapsed=%s",
		copies, copies+1, copies+1, hlsMixedProfileProbeCount(t, countPath), time.Since(coldStarted))
	// The owner reserves one Data session. Warm every remaining request
	// session before sampling so first connection creation is excluded.
	capacity := hlsProfileDatabaseCapacities(pool, app)
	held := make([]*pgxpool.Conn, 0, capacity.DataUsable)
	for range capacity.DataUsable {
		connection, err := pool.Acquire(h.f.ctx)
		if err != nil {
			for _, acquired := range held {
				acquired.Release()
			}
			t.Fatal("warm the shared mixed profile database pool")
		}
		held = append(held, connection)
	}
	for _, connection := range held {
		connection.Release()
	}
	var actualJIT, planCacheMode string
	if err := pool.QueryRow(h.f.ctx, `SELECT current_setting('jit'),current_setting('plan_cache_mode')`).Scan(&actualJIT, &planCacheMode); err != nil {
		t.Fatal("read the actual mixed profile PostgreSQL session settings")
	}
	t.Logf("mixed_scan_pool_setup application_filter=%q callers_filter=%q query_exec_mode=%s description_cache_capacity=%d statement_cache_capacity=%d jit=%s plan_cache_mode=%s data_pool_max=%d data_pool_usable=%d control_pool_max=%d application_pool_max=%d observer_pool_max=%d",
		applicationMode, callerFilter, strings.ReplaceAll(configuration.ConnConfig.DefaultQueryExecMode.String(), " ", "_"), configuration.ConnConfig.DescriptionCacheCapacity,
		configuration.ConnConfig.StatementCacheCapacity, actualJIT, planCacheMode, capacity.DataMax, capacity.DataUsable, capacity.ControlMax, capacity.ApplicationMax, h.f.pool.Config().MaxConns)
	client, controls := hlsPriorityProfileClient(t), hlsPriorityProfileClient(t)
	for _, application := range applications {
		for _, callers := range concurrencies {
			t.Run(fmt.Sprintf("application-%t/callers-%d", application, callers), func(t *testing.T) {
				var a, b hlsHTTPGraph
				headers := h.accounts.second.headers
				if application {
					a = applicationMediaRealGraph(t, h, keys[0], h.accounts.viewer.userID)
					b = applicationMediaRealGraph(t, h, keys[1], h.accounts.viewer.userID)
					headers = keys[1].headers
				} else {
					a = h.graph(t, h.accounts.viewer, 0)
					b = h.graph(t, h.accounts.second, 0)
				}
				expected := hlsMixedProfileWarmGraph(t, h, a)
				hlsMixedProfileWarmGraph(t, h, b)
				target := h.server.URL + a.children[0]
				gate := &hlsPriorityProfileWarmGate{ready: make(chan struct{}), release: make(chan struct{})}
				trace.priority.warm.Store(gate)
				warmed := make(chan []hlsPriorityProfileResponse, 1)
				go func() { warmed <- hlsPriorityProfileWave(h.f.ctx, client, target, "warm", 32, 1) }()
				select {
				case <-gate.ready:
				case <-h.f.ctx.Done():
					close(gate.release)
					t.Fatal("mixed profile HTTP prewarming exceeded the deadline")
				}
				close(gate.release)
				hlsPriorityProfileCheck(t, <-warmed, expected)
				app.hls.requests.Wait()
				trace.priority.warm.Store(nil)
				warm := hlsPriorityProfileRequest(h.f.ctx, controls, http.MethodGet, h.server.URL+"/emby/System/Info", "", nil, headers)
				if warm.err != nil || warm.status != http.StatusOK {
					t.Fatal("warm the independent control HTTP connection")
				}
				probeBefore := hlsMixedProfileProbeCount(t, countPath)
				current := &hlsPriorityProfileCase{}
				trace.priority.current.Store(current)
				trace.backgroundSQL.Store(0)
				trace.backgroundBegins.Store(0)
				trace.cachedCheckpoints.Store(0)
				poolBefore := pool.Stat()
				controlBefore := hlsProfileControlCounters(app)
				trace.enabled.Store(true)
				trace.controlOverlap.Store(0)
				controller := hlsMixedProfileStartScans(h.f.ctx, app, h.f.pool, actor, definition.ID, h.libraryID, fmt.Sprintf("%t-%d", application, callers))
				t.Cleanup(controller.finish)
				select {
				case <-controller.ready:
				case <-controller.done:
					t.Fatalf("mixed task ended before its running checkpoint: error_type=%T", controller.result.err)
				case <-h.f.ctx.Done():
					t.Fatal("mixed warm scan did not reach its real task-owned running state")
				}
				const requests = 256
				getStarted := time.Now()
				type waveResult struct {
					samples []hlsPriorityProfileResponse
					ended   time.Time
				}
				loaded := make(chan waveResult, 1)
				go func() {
					samples := hlsPriorityProfileWave(h.f.ctx, client, target, "steady", callers, requests/callers)
					ended := time.Now()
					// Stop further admissions as soon as the fixed GET workload ends.
					// Joining and terminal verification remain in the parent goroutine.
					controller.stopOnce.Do(func() { close(controller.stop) })
					loaded <- waveResult{samples: samples, ended: ended}
				}()
				for current.steady.sourceReads.Load() == 0 || !hlsMixedProfileScanRunning(h.f.ctx, h.f.pool, h.libraryID) {
					select {
					case <-controller.done:
						t.Fatalf("mixed task ended during the GET window: error_type=%T", controller.result.err)
					default:
					}
					if h.f.ctx.Err() != nil {
						t.Fatal("GET and real scan did not overlap within the profile deadline")
					}
					time.Sleep(time.Millisecond)
				}
				pingStarted := time.Now()
				ping := hlsPriorityProfileRequest(h.f.ctx, controls, http.MethodPost, h.server.URL+"/emby/Sessions/Playing/Ping?PlaySessionId="+b.playID, "stop_get", nil, headers)
				if ping.err != nil || ping.status != http.StatusNoContent {
					t.Fatal("independent Ping failed under mixed scan and GET pressure")
				}
				if current.steady.active.Load() == 0 || !hlsMixedProfileScanRunning(h.f.ctx, h.f.pool, h.libraryID) {
					t.Fatal("mixed work ended before Stopped; this sample cannot claim control competition")
				}
				body, err := json.Marshal(map[string]any{"PlaySessionId": b.playID, "ItemId": h.item.ID, "MediaSourceId": media.SourceID(h.item.ID), "PositionTicks": 0})
				if err != nil {
					t.Fatal("encode the independent playback stop")
				}
				stopStarted := time.Now()
				stopped := hlsPriorityProfileRequest(h.f.ctx, controls, http.MethodPost, h.server.URL+"/emby/Sessions/Playing/Stopped", "stopped", body, headers)
				if stopped.err != nil || stopped.status != http.StatusNoContent {
					t.Fatal("independent Stopped failed under mixed scan and GET pressure")
				}
				if trace.controlOverlap.Load() == 0 {
					t.Fatal("Stopped did not enter while the independent cached GET workload was active")
				}
				wave := <-loaded
				getEnded := wave.ended
				latencies := hlsPriorityProfileCheck(t, wave.samples, expected)
				hlsPriorityProfileWaitIdle(t, &current.steady, &current.stoppingGET, &current.stopped)
				controller.finish()
				poolAfter := pool.Stat()
				controlAfter := hlsProfileControlCounters(app)
				trace.enabled.Store(false)
				trace.priority.current.Store(nil)
				hlsProfileLogDatabaseDelta(t, "mixed-wave-and-controls", application, callers, capacity, hlsProfileCounters(poolBefore), hlsProfileCounters(poolAfter), controlBefore, controlAfter)
				if controller.result.err != nil || hlsMixedProfileProbeCount(t, countPath) != probeBefore {
					t.Fatalf("mixed warm task failed or repeated ffprobe: error_type=%T probe_delta=%d", controller.result.err, hlsMixedProfileProbeCount(t, countPath)-probeBefore)
				}
				if current.steady.requests.Load() != requests || current.steady.sourceReads.Load() != 2*requests || current.steady.playbackReads.Load() != 2*requests {
					t.Fatal("mixed GET samples bypassed real cached HLS authorization")
				}
				var overlap, scanDuration time.Duration
				completed, cancelled, scanned, cancelledScanned, unadmitted, parentCancelled := 0, 0, 0, 0, 0, 0
				pingDuringScan, stopDuringScan := false, false
				for _, record := range controller.result.runs {
					job := record.job
					overlap += hlsMixedProfileOverlap(job, getStarted, getEnded)
					if job.StartedAt != nil && job.FinishedAt != nil {
						scanDuration += job.FinishedAt.Sub(*job.StartedAt)
						pingDuringScan = pingDuringScan || !pingStarted.Before(*job.StartedAt) && pingStarted.Before(*job.FinishedAt)
						stopDuringScan = stopDuringScan || !stopStarted.Before(*job.StartedAt) && stopStarted.Before(*job.FinishedAt)
					}
					if record.run.State == tasks.RunCancelled {
						parentCancelled++
					}
					if job.Status == "NotAdmitted" {
						unadmitted++
					} else if job.Status == "Completed" {
						if job.Status != "Completed" || job.Scanned != copies+1 || job.Added != 0 || job.Updated != 0 {
							t.Fatal("a completed warm task did not visit exactly the unchanged corpus")
						}
						completed++
						scanned += job.Scanned
					} else {
						if job.Status != "Cancelled" || !job.CancelRequested || job.Scanned > copies+1 || job.Added != 0 || job.Updated != 0 {
							t.Fatal("the final mixed scan did not preserve bounded cancellation counters")
						}
						cancelled++
						cancelledScanned += job.Scanned
					}
				}
				if overlap <= 0 || !pingDuringScan || !stopDuringScan || completed+cancelled == 0 {
					t.Fatal("persisted scan times did not establish real GET, Ping and Stopped overlap")
				}
				lateB := hlsPriorityProfileRequest(h.f.ctx, controls, http.MethodGet, h.server.URL+b.children[0], "", nil, nil)
				if lateB.err != nil || lateB.status != http.StatusNotFound {
					t.Fatal("independent stopped playback retained cached delivery")
				}
				hlsPriorityProfileCheck(t, []hlsPriorityProfileResponse{hlsPriorityProfileRequest(h.f.ctx, client, http.MethodGet, target, "", nil, nil)}, expected)
				var state string
				if err := h.f.pool.QueryRow(h.f.ctx, `SELECT state FROM play_sessions WHERE id=$1`, b.playID).Scan(&state); err != nil || state != "Stopped" {
					t.Fatal("independent Stopped did not persist its exact play state")
				}
				t.Logf("mixed_scan_cached_hls application_key=%t callers=%d requests=%d corpus_media=%d segment_bytes=%d request_pool_max=%d request_pool_usable=%d observer_pool_max=%d get_elapsed=%s get_p95=%s get_p99=%s get_sql=%d ping=%s ping_sql=%d stopped=%s stopped_sql=%d stop_get_overlap=%d warm_probe_delta=0 task_runs=%d parent_cancelled=%d unadmitted=%d scan_completed=%d scan_cancelled=%d completed_scanned=%d cancelled_scanned=%d scan_job_duration=%s scan_get_overlap=%s background_sql=%d background_begins=%d cached_checkpoints=%d pool_acquires=%d pool_empty_acquires=%d pool_acquire_duration=%s pool_new_connections=%d",
					application, callers, requests, copies+1, len(expected), capacity.DataMax, capacity.DataUsable, h.f.pool.Config().MaxConns, getEnded.Sub(getStarted), hlsPriorityProfilePercentile(latencies, 95), hlsPriorityProfilePercentile(latencies, 99),
					current.steady.sql.Load(), ping.duration, current.stoppingGET.sql.Load(), stopped.duration, current.stopped.sql.Load(), trace.controlOverlap.Load(), len(controller.result.runs), parentCancelled, unadmitted, completed, cancelled, scanned, cancelledScanned,
					scanDuration, overlap, trace.backgroundSQL.Load(), trace.backgroundBegins.Load(), trace.cachedCheckpoints.Load(), poolAfter.AcquireCount()-poolBefore.AcquireCount(),
					poolAfter.EmptyAcquireCount()-poolBefore.EmptyAcquireCount(), poolAfter.AcquireDuration()-poolBefore.AcquireDuration(), poolAfter.NewConnsCount()-poolBefore.NewConnsCount())
			})
		}
	}
}
