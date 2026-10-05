//go:build linux

package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/tasks"
	"github.com/moooyo/goby/internal/transcode"
)

const (
	hlsColdCopies       = 128
	hlsColdSamplesLimit = 32768
	hlsColdAuditBytes   = 1 << 20
)

type hlsColdProbeEvent struct {
	Kind       string    `json:"kind"`
	ID         string    `json:"id"`
	Key        string    `json:"key"`
	PID        int       `json:"pid"`
	StartTicks uint64    `json:"start_ticks"`
	At         time.Time `json:"at"`
	Status     int       `json:"status"`
}

type hlsColdProbeInterval struct {
	start, end hlsColdProbeEvent
}

func hlsColdFileKey(info os.FileInfo) (string, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Nlink != 1 {
		return "", errors.New("cold fixture requires an independent regular file")
	}
	return fmt.Sprintf("%d:%d:%d", uint64(stat.Dev), stat.Ino, info.Size()), nil
}

func hlsColdPID(pid int) (byte, uint64, error) {
	encoded, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, 0, err
	}
	closing := bytes.LastIndexByte(encoded, ')')
	if closing < 0 {
		return 0, 0, errors.New("invalid primary process observation")
	}
	fields := strings.Fields(string(encoded[closing+1:]))
	if len(fields) < 20 || len(fields[0]) != 1 {
		return 0, 0, errors.New("incomplete primary process observation")
	}
	ticks, err := strconv.ParseUint(fields[19], 10, 64)
	return fields[0][0], ticks, err
}

func hlsColdAppendEvent(path string, event hlsColdProbeEvent) error {
	encoded, err := json.Marshal(event)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0600)
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	written, writeErr := file.Write(encoded)
	return errors.Join(writeErr, file.Close(), func() error {
		if written != len(encoded) {
			return io.ErrShortWrite
		}
		return nil
	}())
}

// The shell wrapper selects this test by argv, because production descriptor
// probes intentionally remove unapproved environment variables. Every helper
// branch exits explicitly: Go test's PASS output must not pollute ffprobe JSON.
// Both variants pay the same launcher and audit work. This is not an absolute
// production ffprobe-speed measurement.
func TestHLSColdPrimaryProbeRecorderHelper(t *testing.T) {
	separator := -1
	for i, argument := range os.Args {
		if argument == "--" {
			separator = i
			break
		}
	}
	if separator < 0 {
		t.Skip("only the transparent descriptor-probe launcher selects this helper")
	}
	fail := func() { _, _ = os.Stderr.WriteString("transparent primary probe recording failed\n"); os.Exit(125) }
	if len(os.Args) < separator+4 {
		fail()
	}
	executable, audit, arguments := os.Args[separator+1], os.Args[separator+2], os.Args[separator+3:]
	flags := make(map[string]bool, 3)
	input := false
	for i, argument := range arguments {
		flags[argument] = true
		input = input || argument == "-i" && i+1 < len(arguments) && arguments[i+1] == "/proc/self/fd/3"
	}
	primary := flags["-show_format"] && flags["-show_streams"] && flags["-show_chapters"] && input
	var inherited *os.File
	var key string
	if target, err := os.Readlink("/proc/self/fd/3"); err == nil && filepath.IsAbs(target) {
		candidate := os.NewFile(3, "inherited media source")
		if info, err := candidate.Stat(); err == nil && info.Mode().IsRegular() {
			inherited = candidate
			if primary {
				key, err = hlsColdFileKey(info)
				if err != nil {
					fail()
				}
			}
		}
	}
	if primary && inherited == nil {
		fail()
	}
	command := exec.Command(executable, arguments...)
	command.Stdout, command.Stderr = os.Stdout, os.Stderr
	command.WaitDelay = time.Second
	if inherited != nil {
		command.ExtraFiles = []*os.File{inherited}
	}
	// Keep the inherited process group. The production parent's group retirement
	// owns this launcher and real child together, including cancellation.
	if err := command.Start(); err != nil {
		fail()
	}
	start := hlsColdProbeEvent{}
	if primary {
		_, ticks, err := hlsColdPID(command.Process.Pid)
		if err != nil || ticks == 0 {
			_ = command.Process.Kill()
			_ = command.Wait()
			fail()
		}
		start = hlsColdProbeEvent{Kind: "start", Key: key, PID: command.Process.Pid, StartTicks: ticks, At: time.Now().UTC()}
		start.ID = fmt.Sprintf("%d-%d", start.PID, start.At.UnixNano())
		if err := hlsColdAppendEvent(audit, start); err != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
			fail()
		}
	}
	waitErr := command.Wait()
	status := 0
	if waitErr != nil {
		status = 124
		var exit *exec.ExitError
		if errors.As(waitErr, &exit) && exit.ExitCode() >= 0 {
			status = exit.ExitCode()
		}
	}
	if primary {
		end := start
		end.Kind, end.At, end.Status = "end", time.Now().UTC(), status
		if err := hlsColdAppendEvent(audit, end); err != nil {
			fail()
		}
	}
	os.Exit(status)
}

func hlsColdReadAudit(path string, complete bool) (map[string]hlsColdProbeInterval, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return make(map[string]hlsColdProbeInterval), nil
	}
	if err != nil {
		return nil, err
	}
	encoded, readErr := io.ReadAll(io.LimitReader(file, hlsColdAuditBytes+1))
	if err := errors.Join(readErr, file.Close()); err != nil || len(encoded) > hlsColdAuditBytes {
		return nil, errors.New("bounded primary audit is unavailable")
	}
	lines := bytes.Split(encoded, []byte{'\n'})
	if complete && len(lines[len(lines)-1]) != 0 {
		return nil, errors.New("terminal primary audit has an incomplete record")
	}
	result := make(map[string]hlsColdProbeInterval)
	for _, line := range lines[:len(lines)-1] {
		if len(line) == 0 {
			return nil, errors.New("empty primary audit record")
		}
		var event hlsColdProbeEvent
		if json.Unmarshal(line, &event) != nil || event.ID == "" || event.Key == "" || event.PID <= 0 || event.At.IsZero() {
			return nil, errors.New("invalid primary audit record")
		}
		interval := result[event.ID]
		switch event.Kind {
		case "start":
			if !interval.start.At.IsZero() {
				return nil, errors.New("duplicate primary start")
			}
			interval.start = event
		case "end":
			if interval.start.At.IsZero() || !interval.end.At.IsZero() || interval.start.Key != event.Key || interval.start.PID != event.PID || interval.start.StartTicks != event.StartTicks || event.At.Before(interval.start.At) {
				return nil, errors.New("primary retirement lost its source or process identity")
			}
			interval.end = event
		default:
			return nil, errors.New("unknown primary audit event")
		}
		result[event.ID] = interval
	}
	return result, nil
}

type hlsColdSnapshot struct {
	runID, childID, jobID, linkedJob, linkedChild, childLibrary, jobLibrary  string
	runState, childState, jobState, jobError                                 string
	runScanned, runAdded, runUpdated, childScanned, childAdded, childUpdated int64
	scanned, added, updated, total, children                                 int64
	force, cancelled                                                         bool
	started, finished, runFinished, childFinished                            *time.Time
}

func hlsColdReadSnapshot(ctx context.Context, observer *pgxpool.Pool, runID, libraryID string) (hlsColdSnapshot, error) {
	var record hlsColdSnapshot
	record.runID = runID
	err := observer.QueryRow(ctx, `SELECT r.state,r.total_children,r.scanned,r.added,r.updated,r.finished_at,
		COALESCE(c.id,''),COALESCE(c.library_id,''),COALESCE(c.state,''),COALESCE(c.scan_job_id,''),
		COALESCE(c.scanned,0),COALESCE(c.added,0),COALESCE(c.updated,0),c.finished_at,
		COALESCE(j.id,''),COALESCE(j.task_child_id,''),COALESCE(j.library_id,''),COALESCE(j.status,''),COALESCE(j.error,''),
		COALESCE(j.scanned,0),COALESCE(j.added,0),COALESCE(j.updated,0),COALESCE(j.force_probe,false),
		COALESCE(j.cancel_requested,false),j.started_at,j.finished_at,
		(SELECT count(*) FROM task_run_children child WHERE child.run_id=r.id)
		FROM task_runs r LEFT JOIN task_run_children c ON c.run_id=r.id AND c.library_id=$2
		LEFT JOIN scan_jobs j ON j.id=c.scan_job_id WHERE r.id=$1`, runID, libraryID).
		Scan(&record.runState, &record.total, &record.runScanned, &record.runAdded, &record.runUpdated, &record.runFinished,
			&record.childID, &record.childLibrary, &record.childState, &record.linkedJob,
			&record.childScanned, &record.childAdded, &record.childUpdated, &record.childFinished,
			&record.jobID, &record.linkedChild, &record.jobLibrary, &record.jobState, &record.jobError,
			&record.scanned, &record.added, &record.updated, &record.force, &record.cancelled, &record.started, &record.finished, &record.children)
	if err != nil {
		return record, err
	}
	if record.total != 1 || record.children != 1 || record.childID == "" || record.childLibrary != libraryID ||
		record.jobID != "" && (record.linkedJob != record.jobID || record.linkedChild != record.childID || record.jobLibrary != libraryID || record.force) {
		return record, errors.New("cold task lost its exact child, scan, library or ordinary mode")
	}
	return record, nil
}

func (record hlsColdSnapshot) running() bool {
	return record.runState == string(tasks.RunRunning) && record.childState == string(tasks.ChildRunning) && record.jobState == "Running" && !record.cancelled && record.started != nil
}

func (record hlsColdSnapshot) complete() error {
	if record.runState != string(tasks.RunCompleted) || record.childState != string(tasks.ChildCompleted) || record.jobState != "Completed" ||
		record.started == nil || record.finished == nil || record.runFinished == nil || record.childFinished == nil || record.cancelled || record.force || record.jobError != "" ||
		record.scanned != hlsColdCopies+1 || record.added != hlsColdCopies || record.updated != 0 ||
		record.runScanned != record.scanned || record.childScanned != record.scanned || record.runAdded != record.added || record.childAdded != record.added || record.runUpdated != 0 || record.childUpdated != 0 {
		return errors.New("cold task did not completely publish its exact new corpus")
	}
	return nil
}

type hlsColdControlEntry struct {
	at, observedAt time.Time
	probeID        string
	activeGET      int64
	pid, state     int
	startTicks     uint64
}

type hlsColdCase struct {
	counts               hlsPriorityProfileCase
	audit                string
	manifest             map[string]string
	mu                   sync.Mutex
	ping                 hlsColdControlEntry
	stop                 hlsColdControlEntry
	pingAfter, stopAfter time.Time
}

func (current *hlsColdCase) liveProbe(after time.Time) (hlsColdControlEntry, error) {
	entry := hlsColdControlEntry{at: time.Now().UTC(), activeGET: current.counts.steady.active.Load()}
	intervals, err := hlsColdReadAudit(current.audit, false)
	if err != nil {
		return entry, err
	}
	for id, interval := range intervals {
		if _, owned := current.manifest[interval.start.Key]; !owned || !interval.end.At.IsZero() || interval.start.StartTicks == 0 || interval.start.At.Before(after) {
			continue
		}
		state, ticks, err := hlsColdPID(interval.start.PID)
		if err != nil || state == 'Z' || state == 'X' || ticks != interval.start.StartTicks {
			continue
		}
		entry.probeID, entry.pid, entry.state, entry.startTicks = id, interval.start.PID, int(state), ticks
		entry.observedAt = time.Now().UTC()
		break
	}
	return entry, nil
}

type hlsColdTracer struct {
	priority                        hlsPriorityProfileTracer
	current                         atomic.Pointer[hlsColdCase]
	enabled                         atomic.Bool
	backgroundSQL, backgroundBegins atomic.Int64
}

func (trace *hlsColdTracer) TraceAcquireStart(ctx context.Context, pool *pgxpool.Pool, data pgxpool.TraceAcquireStartData) context.Context {
	return trace.priority.TraceAcquireStart(ctx, pool, data)
}

func (trace *hlsColdTracer) TraceAcquireEnd(ctx context.Context, pool *pgxpool.Pool, data pgxpool.TraceAcquireEndData) {
	trace.priority.TraceAcquireEnd(ctx, pool, data)
}

func (trace *hlsColdTracer) TraceQueryStart(ctx context.Context, connection *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = trace.priority.TraceQueryStart(ctx, connection, data)
	trace.background(ctx, data.SQL)
	return ctx
}

func (trace *hlsColdTracer) background(ctx context.Context, sql string) {
	if trace.enabled.Load() && ctx.Value(hlsPriorityProfileContextKey{}) == nil {
		trace.backgroundSQL.Add(1)
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(sql)), "begin") {
			trace.backgroundBegins.Add(1)
		}
	}
}

func (trace *hlsColdTracer) TraceQueryEnd(ctx context.Context, connection *pgx.Conn, data pgx.TraceQueryEndData) {
	trace.priority.TraceQueryEnd(ctx, connection, data)
}

func (trace *hlsColdTracer) TraceBatchStart(ctx context.Context, connection *pgx.Conn, data pgx.TraceBatchStartData) context.Context {
	return trace.priority.TraceBatchStart(ctx, connection, data)
}

func (trace *hlsColdTracer) TraceBatchQuery(ctx context.Context, connection *pgx.Conn, data pgx.TraceBatchQueryData) {
	trace.priority.TraceBatchQuery(ctx, connection, data)
	trace.background(ctx, data.SQL)
}

func (trace *hlsColdTracer) TraceBatchEnd(ctx context.Context, connection *pgx.Conn, data pgx.TraceBatchEndData) {
	trace.priority.TraceBatchEnd(ctx, connection, data)
}

func (trace *hlsColdTracer) wrap(next http.Handler) http.Handler {
	marked := trace.priority.wrap(next)
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		phase := request.Header.Get(hlsPriorityProfileHeader)
		if current := trace.current.Load(); current != nil && (phase == "stop_get" || phase == "stopped") {
			current.mu.Lock()
			after := current.pingAfter
			if phase == "stopped" {
				after = current.stopAfter
			}
			current.mu.Unlock()
			entry, _ := current.liveProbe(after)
			current.mu.Lock()
			if phase == "stop_get" {
				current.ping = entry
			} else {
				current.stop = entry
			}
			current.mu.Unlock()
		}
		marked.ServeHTTP(w, request)
	})
}

type hlsColdSample struct {
	started, ended time.Time
	duration       time.Duration
	bytes          int
}

type hlsColdWave struct {
	stop, done chan struct{}
	stopOnce   sync.Once
	cancel     context.CancelFunc
	issued     atomic.Int64
	completed  atomic.Int64
	mu         sync.Mutex
	err        error
	samples    []hlsColdSample // Written before done; never retain response bodies.
}

func (wave *hlsColdWave) problem() error {
	wave.mu.Lock()
	defer wave.mu.Unlock()
	return wave.err
}

func (wave *hlsColdWave) fail(err error) {
	wave.mu.Lock()
	if wave.err == nil {
		wave.err = err
	}
	wave.mu.Unlock()
	wave.cancel()
}

func hlsColdStartWave(ctx context.Context, client *http.Client, target string, callers int, expected []byte) *hlsColdWave {
	work, cancel := context.WithCancel(ctx)
	wave := &hlsColdWave{stop: make(chan struct{}), done: make(chan struct{}), cancel: cancel}
	wantBytes, wantHash := len(expected), sha256.Sum256(expected)
	go func() {
		defer close(wave.done)
		results := make(chan []hlsColdSample, callers)
		var workers sync.WaitGroup
		for range callers {
			workers.Add(1)
			go func() {
				defer workers.Done()
				local := make([]hlsColdSample, 0, 256)
				defer func() { results <- local }()
				for {
					select {
					case <-wave.stop:
						return
					case <-work.Done():
						return
					default:
					}
					if wave.issued.Add(1) > hlsColdSamplesLimit {
						wave.fail(errors.New("cold GET workload reached its finite sample cap"))
						return
					}
					started := time.Now().UTC()
					sample := hlsPriorityProfileRequest(work, client, http.MethodGet, target, "steady", nil, nil)
					ended := time.Now().UTC()
					if sample.err != nil || sample.status != http.StatusOK || len(sample.data) != wantBytes || sha256.Sum256(sample.data) != wantHash {
						wave.fail(errors.New("cold-load cached response failed its status, length or digest check"))
						return
					}
					local = append(local, hlsColdSample{started: started, ended: ended, duration: sample.duration, bytes: len(sample.data)})
					sample.data = nil
					wave.completed.Add(1)
				}
			}()
		}
		workers.Wait()
		close(results)
		for local := range results {
			wave.samples = append(wave.samples, local...)
		}
	}()
	return wave
}

func (wave *hlsColdWave) requestStop(cancel bool) {
	if wave == nil {
		return
	}
	wave.stopOnce.Do(func() { close(wave.stop) })
	if cancel {
		wave.cancel()
	}
}

func (wave *hlsColdWave) join(ctx context.Context) error {
	if wave == nil {
		return nil
	}
	select {
	case <-wave.done:
		wave.cancel()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (wave *hlsColdWave) finish(ctx context.Context, cancel bool) error {
	wave.requestStop(cancel)
	return wave.join(ctx)
}

func hlsColdWaitProbe(ctx context.Context, observer *pgxpool.Pool, current *hlsColdCase, wave *hlsColdWave, runID, libraryID string, after time.Time) error {
	for {
		if wave != nil && wave.problem() != nil {
			return wave.problem()
		}
		record, err := hlsColdReadSnapshot(ctx, observer, runID, libraryID)
		if err != nil {
			return err
		}
		if !tasks.RunState(record.runState).Active() || record.cancelled || record.jobState == "Failed" {
			return errors.New("cold task ended before the required real control/probe overlap")
		}
		entry, err := current.liveProbe(after)
		if err != nil {
			return err
		}
		if record.running() && entry.probeID != "" && (wave == nil || entry.activeGET > 0) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
}

func hlsColdValidateEntry(entry hlsColdControlEntry, intervals map[string]hlsColdProbeInterval, job hlsColdSnapshot) error {
	interval, exists := intervals[entry.probeID]
	if !exists || entry.activeGET <= 0 || entry.pid != interval.start.PID || entry.startTicks == 0 || entry.startTicks != interval.start.StartTicks ||
		entry.state == int('Z') || entry.state == int('X') || interval.end.At.IsZero() || entry.at.Before(interval.start.At) || entry.at.After(interval.end.At) ||
		entry.observedAt.Before(entry.at) || entry.observedAt.After(interval.end.At) || job.started == nil || job.finished == nil || entry.at.Before(*job.started) || entry.at.After(*job.finished) {
		return errors.New("control entry did not overlap a live matching primary process, cached GET and complete scan interval")
	}
	return nil
}

func hlsColdRevision(ctx context.Context, observer *pgxpool.Pool, playID string) (int64, bool, error) {
	var raw *string
	if err := observer.QueryRow(ctx, "SELECT to_jsonb(play)->>'playback_revision' FROM play_sessions play WHERE id=$1", playID).Scan(&raw); err != nil {
		return 0, false, err
	}
	if raw == nil {
		return 0, false, nil
	}
	value, err := strconv.ParseInt(*raw, 10, 64)
	if err != nil || value < 0 {
		return 0, false, errors.New("invalid playback demand revision")
	}
	return value, true, nil
}

func hlsColdExport(t *testing.T, current *hlsColdCase, intervals map[string]hlsColdProbeInterval, job hlsColdSnapshot, samples []hlsColdSample, application bool, callers int, ping, stopped hlsPriorityProfileResponse, sourceAdmission map[string]uint64) {
	t.Helper()
	events := make([]hlsColdProbeEvent, 0, 2*len(intervals))
	for _, interval := range intervals {
		events = append(events, interval.start, interval.end)
	}
	sort.Slice(events, func(i, j int) bool { return events[i].At.Before(events[j].At) })
	current.mu.Lock()
	control := func(entry hlsColdControlEntry) map[string]any {
		return map[string]any{"server_entry": entry.at, "process_observation": entry.observedAt, "primary_id": entry.probeID, "pid": entry.pid, "start_ticks": entry.startTicks, "state": entry.state, "active_cached_get": entry.activeGET}
	}
	pingEntry, stopEntry := control(current.ping), control(current.stop)
	current.mu.Unlock()
	type compactSample struct {
		Started    time.Time `json:"started_at"`
		Ended      time.Time `json:"ended_at"`
		DurationNS int64     `json:"duration_ns"`
		Bytes      int       `json:"bytes"`
	}
	compact := make([]compactSample, 0, len(samples))
	for _, sample := range samples {
		compact = append(compact, compactSample{Started: sample.started, Ended: sample.ended, DurationNS: sample.duration.Nanoseconds(), Bytes: sample.bytes})
	}
	encoded, err := json.Marshal(map[string]any{
		"application_key": application, "callers": callers, "run_id": job.runID, "child_id": job.childID, "scan_job_id": job.jobID,
		"scan_started_at": job.started, "scan_finished_at": job.finished, "scanned": job.scanned, "added": job.added, "updated": job.updated,
		"primary_source_manifest": current.manifest, "primary_events": events, "ping_entry": pingEntry, "stopped_entry": stopEntry,
		"ping_ns": ping.duration.Nanoseconds(), "stopped_ns": stopped.duration.Nanoseconds(), "get_samples": compact,
		"recorder_cost_included": true, "encoder_reap_measured": false,
		"source_admission": sourceAdmission,
	})
	if err != nil {
		t.Fatal("encode immutable completed cold evidence")
	}
	root := os.Getenv("GOBY_COLD_MIXED_EXPORT_ROOT")
	if root == "" {
		// Preserve raw provenance even when the caller chooses only test logs.
		t.Logf("completed_cold_evidence_json=%s", encoded)
		return
	}
	info, err := os.Lstat(root)
	if err != nil || !filepath.IsAbs(root) || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		t.Fatal("GOBY_COLD_MIXED_EXPORT_ROOT must name an existing private absolute directory")
	}
	file, err := os.CreateTemp(root, fmt.Sprintf("cold-application-%t-callers-%d-*.json", application, callers))
	if err != nil {
		t.Fatal("create the retained cold evidence artifact")
	}
	written, writeErr := file.Write(append(encoded, '\n'))
	if err := errors.Join(writeErr, file.Close()); err != nil || written != len(encoded)+1 {
		t.Fatal("persist the complete bounded cold evidence artifact")
	}
	t.Logf("completed_cold_evidence_file=%s", file.Name())
}

// Each case has a new schema, server generation and unindexed corpus. This file
// copies unchanged to baseline; production capacity differences use existing
// candidate-only hooks. Cached B's Stop tests control/fencing ownership, not
// termination of an already-completed encoder or maximum video throughput.
func TestHTTPCompletedColdScanCachedHLSPerformance(t *testing.T) {
	if os.Getenv("GOBY_HLS_COLD_MIXED_PERFORMANCE") != "1" {
		t.Skip("GOBY_HLS_COLD_MIXED_PERFORMANCE=1 enables completed cold scan and cached HLS competition")
	}
	if os.Getenv("GOBY_TEST_DATABASE_URL") == "" {
		t.Fatal("enabled cold profile requires GOBY_TEST_DATABASE_URL")
	}
	for _, name := range []string{"GOBY_FFMPEG", "GOBY_FFPROBE"} {
		path := os.Getenv(name)
		info, err := os.Stat(path)
		if !filepath.IsAbs(path) || err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			t.Fatalf("enabled cold profile requires an absolute executable %s", name)
		}
	}
	privateRoot := os.Getenv("GOBY_MIXED_FIXTURE_ROOT")
	info, err := os.Lstat(privateRoot)
	if err != nil || !filepath.IsAbs(privateRoot) || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		t.Fatal("GOBY_MIXED_FIXTURE_ROOT must name an existing private absolute directory")
	}
	if root := os.Getenv("GOBY_COLD_MIXED_EXPORT_ROOT"); root != "" {
		info, err := os.Lstat(root)
		if err != nil || !filepath.IsAbs(root) || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
			t.Fatal("GOBY_COLD_MIXED_EXPORT_ROOT must name an existing private absolute directory")
		}
	}
	t.Setenv("TMPDIR", privateRoot)
	if filter := os.Getenv("GOBY_COLD_MIXED_APPLICATION_MODE"); filter != "" && filter != "normal" && filter != "key" {
		t.Fatal("GOBY_COLD_MIXED_APPLICATION_MODE must be empty, normal or key")
	}
	if filter := os.Getenv("GOBY_COLD_MIXED_CALLERS"); filter != "" && filter != "8" && filter != "32" {
		t.Fatal("GOBY_COLD_MIXED_CALLERS must be empty, 8 or 32")
	}
	if value := os.Getenv("GOBY_HLS_PRODUCTION_DATABASE_CAPACITY"); value != "" && value != "0" && value != "1" {
		t.Fatal("GOBY_HLS_PRODUCTION_DATABASE_CAPACITY must be empty, 0 or 1")
	}
	for _, application := range []bool{false, true} {
		if filter := os.Getenv("GOBY_COLD_MIXED_APPLICATION_MODE"); filter != "" && filter != map[bool]string{false: "normal", true: "key"}[application] {
			continue
		}
		for _, callers := range []int{8, 32} {
			if filter := os.Getenv("GOBY_COLD_MIXED_CALLERS"); filter != "" && filter != strconv.Itoa(callers) {
				continue
			}
			t.Run(fmt.Sprintf("application-%t/callers-%d", application, callers), func(t *testing.T) {
				hlsColdMixedCase(t, application, callers)
			})
		}
	}
}

func hlsColdMixedCase(t *testing.T, application bool, callers int) {
	h, owner := hlsColdOwnedFixture(t)
	keys := hlsColdIssueKeys(t, h.f, owner, h.accounts.admin.headers.Get("X-Emby-Token"))
	credentialIDs := []string{keys[0].principal.SessionID, keys[1].principal.SessionID}
	for _, statement := range []string{"UPDATE sessions SET client_name='Cold HLS application' WHERE id=ANY($1::text[])", "UPDATE application_key_clients SET client_name='Cold HLS application' WHERE credential_id=ANY($1::text[])"} {
		if _, err := h.f.pool.Exec(h.f.ctx, statement, credentialIDs); err != nil {
			t.Fatal("normalize stable application metadata")
		}
	}
	fixtureRoot := filepath.Join(owner.root, "recorder")
	if err := os.Mkdir(fixtureRoot, 0700); err != nil {
		t.Fatal(err)
	}
	audit, wrapper := filepath.Join(fixtureRoot, "primary.jsonl"), filepath.Join(fixtureRoot, "ffprobe-primary.sh")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
	script := "#!/bin/sh\nexec " + quote(executable) + " -test.run='^TestHLSColdPrimaryProbeRecorderHelper$' -- " + quote(h.ffprobe) + " " + quote(audit) + " \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := h.f.app.Close(h.f.ctx); err != nil {
		t.Fatal("join the original application before reconstructing the cold profile")
	}
	trace := &hlsColdTracer{}
	configuration := h.f.pool.Config()
	configuration.MaxConns, configuration.ConnConfig.Tracer = 16, trace
	hlsProfileConfigureData(configuration)
	pool, err := pgxpool.NewWithConfig(h.f.ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	owner.data = pool
	cfg := h.f.cfg
	cfg.FFprobePath = wrapper
	users := identity.New(pool)
	var options []Option
	if hlsColdPoolOptionsHook != nil {
		options, owner.control, err = hlsColdPoolOptionsHook(h.f.ctx, pool)
		if err != nil {
			t.Fatal("open only the cold fixture's coordinated Control reservation")
		}
	}
	app, err := New(h.f.ctx, cfg, pool, users, h.f.log, "complete-cold-hls-profile", options...)
	if err != nil {
		t.Fatal("construct the complete production server for cold competition")
	}
	owner.app = app
	owner.apps = append(owner.apps, app)
	h.f.app, h.f.users, h.f.handler = app, users, app.Handler()
	h.server = httptest.NewServer(trace.wrap(h.f.handler))
	owner.server = h.server
	principal, err := users.ResolveEmby(h.f.ctx, h.accounts.admin.headers.Get("X-Emby-Token"))
	if err != nil {
		t.Fatal("restore the real task administrator")
	}
	actor := tasks.Actor{Principal: principal, Audience: identity.AdministratorEmby}
	owner.actor = actor
	definition, err := app.taskStore.GetByKey(h.f.ctx, tasks.LibraryScanKey)
	if err != nil {
		t.Fatal(err)
	}
	var a, b hlsHTTPGraph
	headers := h.accounts.second.headers
	if application {
		a, b = applicationMediaRealGraph(t, h, keys[0], h.accounts.viewer.userID), applicationMediaRealGraph(t, h, keys[1], h.accounts.viewer.userID)
		headers = keys[1].headers
	} else {
		a, b = h.graph(t, h.accounts.viewer, 0), h.graph(t, h.accounts.second, 0)
	}
	expected := hlsMixedProfileWarmGraph(t, h, a)
	hlsMixedProfileWarmGraph(t, h, b)
	app.hls.mu.Lock()
	bSession := app.hls.sessions[b.hlsID]
	app.hls.mu.Unlock()
	if bSession == nil {
		t.Fatal("cached B lost its exact session before measured Stop")
	}
	bSession.mu.Lock()
	bProducers := append([]hlsProducer(nil), bSession.producers...)
	bSession.mu.Unlock()
	if len(bProducers) == 0 {
		t.Fatal("cached B has no actual completed producer to fence")
	}
	requests, requestsCancel := context.WithTimeout(h.f.ctx, 5*time.Second)
	joinErr := hlsColdBounded(requests, app.hls.requests.Wait)
	requestsCancel()
	if joinErr != nil {
		t.Fatal("cached warmup request owners did not retire before cold admission")
	}
	client, controls := owner.client(), owner.client()
	hlsPriorityProfileCheck(t, hlsPriorityProfileWave(h.f.ctx, client, h.server.URL+a.children[0], "", callers, 1), expected)
	if response := hlsPriorityProfileRequest(h.f.ctx, controls, http.MethodGet, h.server.URL+"/emby/System/Info", "", nil, headers); response.err != nil || response.status != http.StatusOK {
		t.Fatal("warm the independent control HTTP connection")
	}
	capacity := hlsProfileDatabaseCapacities(pool, app)
	if capacity.ApplicationMax != 16 || capacity.DataUsable != capacity.DataMax-1 {
		t.Fatal("profile silently expanded the sixteen-connection application budget")
	}
	if owner.control != nil {
		reserved := make([]*pgxpool.Conn, 0, capacity.ControlMax)
		for range capacity.ControlMax {
			connection, err := owner.control.Acquire(h.f.ctx)
			if err != nil {
				for _, previous := range reserved {
					previous.Release()
				}
				t.Fatal("warm the cold fixture's entire coordinated Control pool")
			}
			reserved = append(reserved, connection)
		}
		for _, connection := range reserved {
			connection.Release()
		}
	}
	held := make([]*pgxpool.Conn, 0, capacity.DataUsable)
	for range capacity.DataUsable {
		connection, err := pool.Acquire(h.f.ctx)
		if err != nil {
			for _, previous := range held {
				previous.Release()
			}
			t.Fatal("warm every Data session before creating the new corpus")
		}
		held = append(held, connection)
	}
	for _, connection := range held {
		connection.Release()
	}
	if err := os.WriteFile(audit, nil, 0600); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(h.path)
	if err != nil {
		t.Fatal(err)
	}
	current := &hlsColdCase{audit: audit, manifest: make(map[string]string, hlsColdCopies)}
	paths := make([]string, 0, hlsColdCopies)
	for i := 0; i < hlsColdCopies; i++ {
		path := filepath.Join(filepath.Dir(h.path), fmt.Sprintf("Cold.Mixed.Copy.%03d.mp4", i))
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatal("create a distinct unindexed cold source")
		}
		written, writeErr := file.Write(source)
		info, statErr := file.Stat()
		closeErr := file.Close()
		if errors.Join(writeErr, statErr, closeErr) != nil || written != len(source) {
			t.Fatal("write the complete cold media file")
		}
		key, err := hlsColdFileKey(info)
		if err != nil || current.manifest[key] != "" {
			t.Fatal("cold corpus has a reused or invalid filesystem identity")
		}
		current.manifest[key] = filepath.Base(path)
		paths = append(paths, path)
	}
	imageCorpus := hlsColdPrepareImages(t, paths)
	var indexed int
	if err := h.f.pool.QueryRow(h.f.ctx, "SELECT count(*) FROM items WHERE library_id=$1 AND path=ANY($2::text[])", h.libraryID, paths).Scan(&indexed); err != nil || indexed != 0 {
		t.Fatal("the cold corpus was already indexed before admission")
	}
	beforeData, beforeControl := hlsProfileCounters(pool.Stat()), hlsProfileControlCounters(app)
	pgObserver := hlsStartPGPhaseObserver(t, h.f.ctx, pool, &trace.priority, map[string]any{"workload": "cold_scan_cached_hls", "application_key": application, "callers": callers})
	sourceAdmissionBefore := hlsProfileSourceAdmissionCounters(app)
	trace.current.Store(current)
	trace.priority.current.Store(&current.counts)
	trace.enabled.Store(true)
	var wave *hlsColdWave
	runID := ""
	admission, err := app.taskManager.Start(h.f.ctx, actor, tasks.StartRequest{TaskID: definition.ID, RequestID: "complete-cold-cached-hls"})
	if err == nil && admission.Admitted && admission.Run.ID != "" {
		runID, owner.runID = admission.Run.ID, admission.Run.ID
	}
	if err != nil || !admission.Admitted || runID == "" || admission.Run.TotalChildren != 1 {
		t.Fatal("admit exactly one real ordinary cold task")
	}
	if err := hlsColdWaitProbe(h.f.ctx, h.f.pool, current, nil, runID, h.libraryID, time.Time{}); err != nil {
		t.Fatal(err)
	}
	getStarted := time.Now().UTC()
	wave = hlsColdStartWave(h.f.ctx, client, h.server.URL+a.children[0], callers, expected)
	owner.wave = wave
	var pingBefore, pingAfter time.Time
	revisionBefore, revisionAvailable, err := hlsColdRevision(h.f.ctx, h.f.pool, b.playID)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.f.pool.QueryRow(h.f.ctx, "SELECT updated_at FROM play_sessions WHERE id=$1", b.playID).Scan(&pingBefore); err != nil {
		t.Fatal(err)
	}
	current.mu.Lock()
	current.pingAfter = getStarted
	current.mu.Unlock()
	if err := hlsColdWaitProbe(h.f.ctx, h.f.pool, current, wave, runID, h.libraryID, getStarted); err != nil {
		t.Fatal(err)
	}
	ping := hlsPriorityProfileRequest(h.f.ctx, controls, http.MethodPost, h.server.URL+"/emby/Sessions/Playing/Ping?PlaySessionId="+b.playID, "stop_get", nil, headers)
	if ping.err != nil || ping.status != http.StatusNoContent {
		t.Fatal("real independent heartbeat failed under cold scan and cached GET load")
	}
	if err := h.f.pool.QueryRow(h.f.ctx, "SELECT updated_at FROM play_sessions WHERE id=$1", b.playID).Scan(&pingAfter); err != nil || !pingAfter.After(pingBefore) {
		t.Fatal("Ping was inert instead of updating its real owned playback")
	}
	revisionAfterPing, pingRevisionAvailable, err := hlsColdRevision(h.f.ctx, h.f.pool, b.playID)
	if err != nil || pingRevisionAvailable != revisionAvailable || revisionAvailable && revisionAfterPing != revisionBefore {
		t.Fatal("heartbeat changed the committed demand revision")
	}
	stopAfter := time.Now().UTC()
	current.mu.Lock()
	current.stopAfter = stopAfter
	current.mu.Unlock()
	if err := hlsColdWaitProbe(h.f.ctx, h.f.pool, current, wave, runID, h.libraryID, stopAfter); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"PlaySessionId": b.playID, "ItemId": h.item.ID, "MediaSourceId": media.SourceID(h.item.ID), "PositionTicks": 0})
	if err != nil {
		t.Fatal(err)
	}
	stopped := hlsPriorityProfileRequest(h.f.ctx, controls, http.MethodPost, h.server.URL+"/emby/Sessions/Playing/Stopped", "stopped", body, headers)
	if stopped.err != nil || stopped.status != http.StatusNoContent {
		t.Fatal("independent cached Stop failed under complete cold competition")
	}
	revisionAfterStop, stopRevisionAvailable, err := hlsColdRevision(h.f.ctx, h.f.pool, b.playID)
	if err != nil || stopRevisionAvailable != revisionAvailable || revisionAvailable && revisionAfterStop != revisionBefore+1 {
		t.Fatal("committed cached Stop did not advance its demand revision exactly once")
	}
	var terminal hlsColdSnapshot
	for {
		if err := wave.problem(); err != nil {
			t.Fatal(err)
		}
		terminal, err = hlsColdReadSnapshot(h.f.ctx, h.f.pool, runID, h.libraryID)
		if err != nil {
			t.Fatal(err)
		}
		if terminal.runState == string(tasks.RunCompleted) {
			if err := terminal.complete(); err != nil {
				t.Fatal(err)
			}
			if wave.completed.Load() >= 256 {
				break
			}
		} else if terminal.runState != string(tasks.RunRunning) && terminal.runState != string(tasks.RunPending) {
			t.Fatal("cold task reached a non-completed terminal state")
		}
		select {
		case <-h.f.ctx.Done():
			t.Fatal("complete cold workload exceeded its finite deadline")
		case <-time.After(20 * time.Millisecond):
		}
	}
	if err := wave.finish(h.f.ctx, false); err != nil {
		t.Fatal("bounded cached GET workload join did not complete")
	}
	getEnded := time.Now().UTC()
	hlsPriorityProfileWaitIdle(t, &current.counts.steady, &current.counts.stoppingGET, &current.counts.stopped)
	afterData, afterControl := hlsProfileCounters(pool.Stat()), hlsProfileControlCounters(app)
	// This process-wide delta includes the cold scan, GET wave, Ping and Stop.
	// Tagged per-request SQL facts remain the evidence for two fresh GET stages.
	sourceAdmission := hlsProfileSourceAdmissionDelta(sourceAdmissionBefore, hlsProfileSourceAdmissionCounters(app))
	trace.enabled.Store(false)
	imageCorpus.verify(t, h.f.ctx, h.f.pool, h.libraryID, h.path, paths)
	intervals, err := hlsColdReadAudit(audit, true)
	if err != nil || len(intervals) != hlsColdCopies {
		t.Fatal("the complete cold task did not run exactly 128 primary metadata probes")
	}
	seen := make(map[string]bool, hlsColdCopies)
	for _, interval := range intervals {
		if current.manifest[interval.start.Key] == "" || seen[interval.start.Key] || interval.start.StartTicks == 0 || interval.end.At.IsZero() || interval.end.Status != 0 {
			t.Fatal("primary probes lost their exact cold source identity or successful retirement")
		}
		seen[interval.start.Key] = true
	}
	current.mu.Lock()
	pingEntry, stopEntry := current.ping, current.stop
	current.mu.Unlock()
	if err := hlsColdValidateEntry(pingEntry, intervals, terminal); err != nil {
		t.Fatal("Ping has no real primary-probe, GET and complete scan overlap")
	}
	if err := hlsColdValidateEntry(stopEntry, intervals, terminal); err != nil {
		t.Fatal("Stopped has no real primary-probe, GET and complete scan overlap")
	}
	var latencies []time.Duration
	var overlapBytes int64
	partial := 0
	for _, sample := range wave.samples {
		if sample.ended.After(*terminal.started) && sample.started.Before(*terminal.finished) {
			latencies = append(latencies, sample.duration)
			overlapBytes += int64(sample.bytes)
			if sample.started.Before(*terminal.started) || sample.ended.After(*terminal.finished) {
				partial++
			}
		}
	}
	if len(latencies) == 0 || len(wave.samples) > hlsColdSamplesLimit || wave.problem() != nil || current.counts.steady.requests.Load() != int64(len(wave.samples)) {
		t.Fatal("cold GET samples were empty, incomplete or outside their memory bound")
	}
	// Every request must independently complete both fresh stages. Baseline
	// canonical clock-bearing reads and candidate full reauthorizations can add
	// observations, so an exact aggregate of two incorrectly rejects valid work.
	minimumReads := 2 * int64(len(wave.samples))
	if current.counts.steady.sourceReads.Load() < minimumReads || current.counts.steady.playbackReads.Load() < minimumReads ||
		current.counts.steady.finalPlaybackReads.Load() < minimumReads || current.counts.steady.freshRequests.Load() != int64(len(wave.samples)) {
		t.Fatalf("cold cached GET workload bypassed a request's fresh authorization stages: samples=%d source=%d playback=%d clock=%d fresh=%d",
			len(wave.samples), current.counts.steady.sourceReads.Load(), current.counts.steady.playbackReads.Load(),
			current.counts.steady.finalPlaybackReads.Load(), current.counts.steady.freshRequests.Load())
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	if response := hlsPriorityProfileRequest(h.f.ctx, controls, http.MethodGet, h.server.URL+b.children[0], "", nil, nil); response.err != nil || response.status != http.StatusNotFound {
		t.Fatal("Stopped retained its exact cached delivery scope")
	}
	hlsPriorityProfileCheck(t, []hlsPriorityProfileResponse{hlsPriorityProfileRequest(h.f.ctx, client, http.MethodGet, h.server.URL+a.children[0], "", nil, nil)}, expected)
	var bState, aState string
	if err := h.f.pool.QueryRow(h.f.ctx, "SELECT state FROM play_sessions WHERE id=$1", b.playID).Scan(&bState); err != nil || bState != "Stopped" {
		t.Fatal("Stopped did not persist its own playback state")
	}
	if err := h.f.pool.QueryRow(h.f.ctx, "SELECT state FROM play_sessions WHERE id=$1", a.playID).Scan(&aState); err != nil || aState == "Stopped" || aState == "Expired" {
		t.Fatal("independent Stop invalidated the active cached playback")
	}
	app.hls.mu.Lock()
	registered := app.hls.sessions[b.hlsID] != nil
	app.hls.mu.Unlock()
	if registered {
		t.Fatal("cached Stop retained its HLS registration")
	}
	if bSession.ctx.Err() == nil {
		t.Fatal("cached Stop retained its exact session lifetime")
	}
	policy := app.playbackPolicyGate()
	policy.mu.Lock()
	retainedPolicy := false
	for _, lease := range policy.leases {
		retainedPolicy = retainedPolicy || lease.scope == bSession.key.scope
	}
	policy.mu.Unlock()
	if retainedPolicy {
		t.Fatal("cached Stop retained its exact delivery policy lease")
	}
	for _, producer := range bProducers {
		handle, err := app.hls.manager.TryOpen(bSession.key.scope, producer.id, "segment-000000.ts")
		if handle != nil {
			_ = handle.Close()
		}
		if handle != nil || !errors.Is(err, transcode.ErrJobCancelled) && !errors.Is(err, transcode.ErrJobNotFound) {
			t.Fatal("cached Stop failed to fence its completed output owner")
		}
	}
	var activeJobs int
	if err := h.f.pool.QueryRow(h.f.ctx, "SELECT count(*) FROM encoding_jobs WHERE play_session_id=$1 AND state IN ('queued','running')", b.playID).Scan(&activeJobs); err != nil || activeJobs != 0 {
		t.Fatal("cached Stop retained an active encoding owner")
	}
	var catalogMedia int
	if err := h.f.pool.QueryRow(h.f.ctx, "SELECT count(*) FROM items WHERE library_id=$1 AND NOT is_folder AND media IS NOT NULL", h.libraryID).Scan(&catalogMedia); err != nil || catalogMedia != hlsColdCopies+1 {
		t.Fatal("partial cold leaves were mistaken for a complete catalog")
	}
	overlapStart, overlapEnd := getStarted, getEnded
	if terminal.started.After(overlapStart) {
		overlapStart = *terminal.started
	}
	if terminal.finished.Before(overlapEnd) {
		overlapEnd = *terminal.finished
	}
	overlap := overlapEnd.Sub(overlapStart)
	if overlap <= 0 {
		t.Fatal("complete cold scan has no persisted GET workload overlap")
	}
	hlsColdExport(t, current, intervals, terminal, wave.samples, application, callers, ping, stopped, sourceAdmission)
	encodedAdmission, err := json.Marshal(sourceAdmission)
	if err != nil {
		t.Fatal("encode aggregate cold source admission measurements")
	}
	t.Logf("completed_cold_source_admission_json=%s", encodedAdmission)
	pgObserver.finish(t)
	hlsProfileLogPhaseTiming(t, &current.counts, map[string]any{"workload": "cold_scan_cached_hls", "application_key": application, "callers": callers})
	t.Logf("cold_playback_demand_revision available=%t before=%d after_ping=%d after_stopped=%d", revisionAvailable, revisionBefore, revisionAfterPing, revisionAfterStop)
	hlsProfileLogDatabaseDelta(t, "completed-cold-wave-and-controls", application, callers, capacity, beforeData, afterData, beforeControl, afterControl)
	t.Logf("complete_cold_cached_hls application_key=%t callers=%d application_pool_max=%d data_pool_max=%d data_usable=%d control_pool_max=%d observer_pool_max=%d corpus_media=%d primary_probes=%d scanned=%d added=%d updated=%d task_state=%s child_state=%s scan_state=%s scan_job_elapsed=%s get_elapsed=%s scan_get_overlap=%s get_samples=%d overlapping_samples=%d partial_samples=%d overlapping_bytes=%d p95=%s p99=%s get_sql=%d get_source_reads=%d get_playback_reads=%d get_entity_projections=%d ping=%s ping_sql=%d stopped=%s stopped_sql=%d ping_active_get=%d stopped_active_get=%d background_sql=%d background_begins=%d recorder=transparent_test_binary_start_wait_plus_live_pid cached_stop_reap_measured=false",
		application, callers, capacity.ApplicationMax, capacity.DataMax, capacity.DataUsable, capacity.ControlMax, h.f.pool.Config().MaxConns,
		hlsColdCopies+1, len(intervals), terminal.scanned, terminal.added, terminal.updated, terminal.runState, terminal.childState, terminal.jobState,
		terminal.finished.Sub(*terminal.started), getEnded.Sub(getStarted), overlap, len(wave.samples), len(latencies), partial, overlapBytes,
		hlsPriorityProfilePercentile(latencies, 95), hlsPriorityProfilePercentile(latencies, 99), current.counts.steady.sql.Load(), current.counts.steady.sourceReads.Load(), current.counts.steady.playbackReads.Load(), current.counts.steady.entityProjections.Load(), ping.duration, current.counts.stoppingGET.sql.Load(), stopped.duration, current.counts.stopped.sql.Load(),
		pingEntry.activeGET, stopEntry.activeGET, trace.backgroundSQL.Load(), trace.backgroundBegins.Load())
}
