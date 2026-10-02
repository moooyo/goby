//go:build linux

package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/transcode"
	"golang.org/x/sys/unix"
)

// These counters are cumulative kernel counters, not estimated utilization.
// Last is a lower bound when the runner reaps between observations. A pidfd,
// the original start tick and the exact owned cwd establish retirement identity.
type hlsWallPauseProcessCounters struct {
	UserTicks   uint64 `json:"user_cpu_ticks"`
	SystemTicks uint64 `json:"system_cpu_ticks"`
	ReadChars   uint64 `json:"read_chars"`
	WriteChars  uint64 `json:"write_chars"`
	ReadBytes   uint64 `json:"read_bytes"`
	WriteBytes  uint64 `json:"write_bytes"`
	OpenFDs     int    `json:"open_fds"`
}

type hlsWallPauseProcessEvidence struct {
	JobID            string                      `json:"job_id"`
	PID              int                         `json:"pid"`
	StartTick        uint64                      `json:"start_tick"`
	FirstMS          int64                       `json:"first_observed_ms"`
	LastMS           int64                       `json:"last_counters_ms"`
	ExitMS           int64                       `json:"pidfd_exit_observed_ms,omitempty"`
	ReapMS           int64                       `json:"reap_observed_ms,omitempty"`
	Samples          int                         `json:"samples"`
	MaxFDs           int                         `json:"max_open_fds"`
	First            hlsWallPauseProcessCounters `json:"first"`
	Last             hlsWallPauseProcessCounters `json:"last_lower_bound"`
	Alive            bool                        `json:"alive"`
	CounterErrorCode string                      `json:"counter_error_code,omitempty"`
	pidfd            int
}

type hlsWallPauseObserver struct {
	mu                 sync.Mutex
	control, root, exe string
	started            time.Time
	processes          map[string]*hlsWallPauseProcessEvidence
	observationError   string
	cancel             context.CancelFunc
	done               chan struct{}
	closeOnce          sync.Once
}

func newHLSWallPauseObserver(t *testing.T, h *hlsHTTPFixture, control string, started time.Time) *hlsWallPauseObserver {
	t.Helper()
	exe, err := exec.LookPath(h.ffmpeg)
	if err == nil {
		exe, err = filepath.Abs(exe)
	}
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		t.Fatalf("resolve owned encoder executable: error_type=%T", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	observer := &hlsWallPauseObserver{control: control, root: h.f.app.cfg.Transcoding.CacheDirectory,
		exe: exe, started: started, processes: make(map[string]*hlsWallPauseProcessEvidence),
		cancel: cancel, done: make(chan struct{})}
	go func() {
		defer close(observer.done)
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for {
			observer.observe()
			select {
			case <-tick.C:
			case <-ctx.Done():
				return
			}
		}
	}()
	t.Cleanup(func() {
		if err := observer.close(); err != nil {
			t.Fatalf("join owned process observer: error_type=%T", err)
		}
	})
	return observer
}

func hlsWallPauseJobID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			if char < 'a' || char > 'f' {
				return false
			}
		}
	}
	return true
}

func hlsWallPauseStat(data []byte) (uint64, hlsWallPauseProcessCounters, error) {
	end := strings.LastIndex(string(data), ") ")
	if end < 0 {
		return 0, hlsWallPauseProcessCounters{}, errors.New("stat delimiter unavailable")
	}
	fields := strings.Fields(string(data[end+2:]))
	if len(fields) < 20 {
		return 0, hlsWallPauseProcessCounters{}, errors.New("stat counters unavailable")
	}
	values := [3]uint64{}
	for index, field := range []int{11, 12, 19} {
		value, err := strconv.ParseUint(fields[field], 10, 64)
		if err != nil {
			return 0, hlsWallPauseProcessCounters{}, errors.New("stat counter invalid")
		}
		values[index] = value
	}
	return values[2], hlsWallPauseProcessCounters{UserTicks: values[0], SystemTicks: values[1]}, nil
}

func hlsWallPauseReadProcess(pid int, start uint64) (hlsWallPauseProcessCounters, error) {
	directory := filepath.Join("/proc", strconv.Itoa(pid))
	stat, err := os.ReadFile(filepath.Join(directory, "stat"))
	if err != nil {
		return hlsWallPauseProcessCounters{}, err
	}
	actualStart, counters, err := hlsWallPauseStat(stat)
	if err != nil || actualStart != start {
		return hlsWallPauseProcessCounters{}, errors.New("process identity changed")
	}
	data, err := os.ReadFile(filepath.Join(directory, "io"))
	if err != nil {
		return hlsWallPauseProcessCounters{}, err
	}
	values := make(map[string]uint64)
	for _, line := range strings.Split(string(data), "\n") {
		name, number, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value, parseErr := strconv.ParseUint(strings.TrimSpace(number), 10, 64)
		if parseErr != nil {
			return hlsWallPauseProcessCounters{}, errors.New("process IO counter invalid")
		}
		values[name] = value
	}
	for _, name := range []string{"rchar", "wchar", "read_bytes", "write_bytes"} {
		if _, present := values[name]; !present {
			return hlsWallPauseProcessCounters{}, errors.New("process IO counter unavailable")
		}
	}
	counters.ReadChars, counters.WriteChars = values["rchar"], values["wchar"]
	counters.ReadBytes, counters.WriteBytes = values["read_bytes"], values["write_bytes"]
	fds, err := os.ReadDir(filepath.Join(directory, "fd"))
	if err != nil {
		return hlsWallPauseProcessCounters{}, err
	}
	counters.OpenFDs = len(fds)
	// Re-read after collecting counters so PID reuse cannot mix two processes.
	stat, err = os.ReadFile(filepath.Join(directory, "stat"))
	if err != nil {
		return hlsWallPauseProcessCounters{}, err
	}
	actualStart, _, err = hlsWallPauseStat(stat)
	if err != nil || actualStart != start {
		return hlsWallPauseProcessCounters{}, errors.New("process identity changed")
	}
	return counters, nil
}

func hlsWallPausePIDFDExited(fd int) (bool, error) {
	for {
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		ready, err := unix.Poll(fds, 0)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || fds[0].Revents&(unix.POLLERR|unix.POLLNVAL) != 0 {
			return false, errors.New("identity-bound process handle unavailable")
		}
		return ready > 0 && fds[0].Revents&(unix.POLLIN|unix.POLLHUP) != 0, nil
	}
}

func (observer *hlsWallPauseObserver) observe() {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	entries, err := os.ReadDir(observer.control)
	if err != nil {
		observer.observationError = "control_inventory_unavailable"
		return
	}
	for _, entry := range entries {
		id := strings.TrimSuffix(entry.Name(), ".pid")
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".pid") || !hlsWallPauseJobID(id) || observer.processes[id] != nil {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(observer.control, entry.Name()))
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
		if readErr != nil || parseErr != nil || pid <= 0 {
			continue
		}
		directory := filepath.Join("/proc", strconv.Itoa(pid))
		cwd, cwdErr := os.Readlink(filepath.Join(directory, "cwd"))
		exe, exeErr := os.Readlink(filepath.Join(directory, "exe"))
		owned, ownedErr := filepath.EvalSymlinks(filepath.Join(observer.root, id))
		if cwdErr != nil || exeErr != nil || ownedErr != nil || cwd != owned || exe != observer.exe {
			continue
		}
		stat, statErr := os.ReadFile(filepath.Join(directory, "stat"))
		start, _, statErr2 := hlsWallPauseStat(stat)
		if statErr != nil || statErr2 != nil {
			continue
		}
		fd, fdErr := unix.PidfdOpen(pid, 0)
		if fdErr != nil {
			observer.observationError = "pidfd_unavailable"
			continue
		}
		counters, counterErr := hlsWallPauseReadProcess(pid, start)
		if counterErr != nil {
			_ = unix.Close(fd)
			continue
		}
		elapsed := time.Since(observer.started).Milliseconds()
		observer.processes[id] = &hlsWallPauseProcessEvidence{JobID: id, PID: pid, StartTick: start,
			FirstMS: elapsed, LastMS: elapsed, Samples: 1, First: counters, Last: counters,
			MaxFDs: counters.OpenFDs, Alive: true, pidfd: fd}
	}
	for _, process := range observer.processes {
		if process.ReapMS != 0 {
			continue
		}
		elapsed := time.Since(observer.started).Milliseconds()
		exited, exitErr := hlsWallPausePIDFDExited(process.pidfd)
		if exitErr != nil {
			observer.observationError = "pidfd_poll_unavailable"
			continue
		}
		if exited {
			process.Alive = false
			if process.ExitMS == 0 {
				process.ExitMS = elapsed
			}
		}
		stat, statErr := os.ReadFile(filepath.Join("/proc", strconv.Itoa(process.PID), "stat"))
		actualStart, _, parseErr := hlsWallPauseStat(stat)
		if exited && (errors.Is(statErr, os.ErrNotExist) || statErr == nil && parseErr == nil && actualStart != process.StartTick) {
			process.ReapMS = elapsed
			continue
		}
		counters, counterErr := hlsWallPauseReadProcess(process.PID, process.StartTick)
		if counterErr != nil {
			// Exit and reap may occur between adjacent proc reads. Retain the
			// last valid counters; retirement is independently checked by pidfd.
			if !exited && !errors.Is(counterErr, os.ErrNotExist) {
				process.CounterErrorCode = "live_counter_unavailable"
			}
			continue
		}
		process.Last, process.LastMS = counters, elapsed
		process.MaxFDs = max(process.MaxFDs, counters.OpenFDs)
		process.Samples++
	}
}

func (observer *hlsWallPauseObserver) snapshot() ([]hlsWallPauseProcessEvidence, string) {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	processes := make([]hlsWallPauseProcessEvidence, 0, len(observer.processes))
	for _, process := range observer.processes {
		processes = append(processes, *process)
	}
	sort.Slice(processes, func(i, j int) bool { return processes[i].JobID < processes[j].JobID })
	return processes, observer.observationError
}

func (observer *hlsWallPauseObserver) close() error {
	observer.closeOnce.Do(observer.cancel)
	select {
	case <-observer.done:
	case <-time.After(5 * time.Second):
		return errors.New("process observer did not join")
	}
	observer.mu.Lock()
	defer observer.mu.Unlock()
	for _, process := range observer.processes {
		if process.pidfd >= 0 {
			_ = unix.Close(process.pidfd)
			process.pidfd = -1
		}
	}
	return nil
}

type hlsWallPauseFile struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
}

type hlsWallPauseOwnedJob struct {
	Role  string
	Scope transcode.Scope
}

type hlsWallPauseScopedUsage struct {
	Role       string                  `json:"role"`
	PlaybackID string                  `json:"playback_id"`
	Usage      transcode.ResourceUsage `json:"usage"`
}

type hlsWallPauseJobSample struct {
	ID            string             `json:"job_id"`
	Role          string             `json:"role"`
	State         string             `json:"state"`
	ErrorCode     string             `json:"error_code,omitempty"`
	ChargedBytes  int64              `json:"record_charged_bytes"`
	LastAccessUTC string             `json:"last_access_utc,omitempty"`
	Directory     bool               `json:"directory_present"`
	ServerFDs     int                `json:"server_fds_into_job"`
	Published     []hlsWallPauseFile `json:"closed_published_files"`
}

type hlsWallPauseSample struct {
	Phase          string                        `json:"phase"`
	ElapsedMS      int64                         `json:"elapsed_ms"`
	CaptureEndMS   int64                         `json:"capture_end_ms"`
	PauseElapsedMS int64                         `json:"pause_elapsed_ms,omitempty"`
	ServerCounters hlsWallPauseProcessCounters   `json:"server_cumulative_counters"`
	Slots          transcode.Metrics             `json:"slots"`
	QueuedRecords  int                           `json:"queued_records"`
	ChargedRecords int64                         `json:"sum_retained_record_charged_bytes"`
	TargetPins     int                           `json:"target_redirect_pins"`
	AllPins        int                           `json:"all_redirect_pins"`
	TargetPaused   bool                          `json:"target_paused"`
	TargetClosed   bool                          `json:"target_closed"`
	Jobs           []hlsWallPauseJobSample       `json:"jobs"`
	Processes      []hlsWallPauseProcessEvidence `json:"owned_processes"`
	ManagerScopes  []hlsWallPauseScopedUsage     `json:"manager_scopes"`
}

type hlsWallPauseEvidence struct {
	Version                  int                           `json:"version"`
	Case                     string                        `json:"case"`
	Subset                   string                        `json:"subset"`
	StartedUTC               string                        `json:"started_utc"`
	PauseRequestedMS         int64                         `json:"pause_requested_ms"`
	PauseAcceptedMS          int64                         `json:"pause_accepted_elapsed_ms"`
	PauseObservedMS          int64                         `json:"pause_observed_ms"`
	RedirectPinsQuietAfterMS int64                         `json:"redirect_pins_quiet_after_pause_ms"`
	CancellationExitDelayMS  int64                         `json:"cancellation_exit_delay_ms"`
	CancellationReapDelayMS  int64                         `json:"cancellation_reap_delay_ms"`
	Completed                bool                          `json:"completed"`
	Failed                   bool                          `json:"failed"`
	Boundary                 []string                      `json:"measurement_boundaries"`
	FirstJobID               string                        `json:"first_job_id,omitempty"`
	CancelledJobID           string                        `json:"cancelled_live_job_id,omitempty"`
	ColdAdmissionStatus      int                           `json:"cancelled_cold_admission_status,omitempty"`
	NaturalTTLLastAccessUTC  string                        `json:"natural_ttl_last_access_utc,omitempty"`
	ResumeJobID              string                        `json:"resume_job_id,omitempty"`
	BeforeSHA256             []string                      `json:"before_sha256,omitempty"`
	ResumeSHA256             []string                      `json:"resume_sha256,omitempty"`
	Samples                  []hlsWallPauseSample          `json:"samples"`
	Processes                []hlsWallPauseProcessEvidence `json:"owned_processes"`
	ObservationError         string                        `json:"observation_error_code,omitempty"`
}

func hlsWallPauseSaveEvidence(t *testing.T, directory string, evidence *hlsWallPauseEvidence, observer *hlsWallPauseObserver) {
	t.Helper()
	evidence.Failed = t.Failed()
	if observer != nil {
		evidence.Processes, evidence.ObservationError = observer.snapshot()
	}
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err == nil {
		temporary := filepath.Join(directory, evidence.Case+".json.tmp")
		err = os.WriteFile(temporary, append(data, '\n'), 0o600)
		if err == nil {
			err = os.Rename(temporary, filepath.Join(directory, evidence.Case+".json"))
		}
	}
	if err != nil {
		t.Errorf("persist safe wall-pause evidence: error_type=%T", err)
	}
}

func hlsWallPauseOwnedDescendants(root string, ids []string) (int, error) {
	owned := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !hlsWallPauseJobID(id) {
			return 0, errors.New("invalid owned job identity")
		}
		owned[filepath.Join(root, id)] = true
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if _, parseErr := strconv.Atoi(entry.Name()); parseErr != nil {
			continue
		}
		cwd, cwdErr := os.Readlink(filepath.Join("/proc", entry.Name(), "cwd"))
		if cwdErr != nil {
			continue
		}
		cwd = strings.TrimSuffix(cwd, " (deleted)")
		for directory := range owned {
			if cwd == directory || strings.HasPrefix(cwd, directory+string(filepath.Separator)) {
				count++
				break
			}
		}
	}
	return count, nil
}

func hlsWallPauseCapture(t *testing.T, h *hlsHTTPFixture, graph hlsGeneratedWindowHTTPGraph,
	observer *hlsWallPauseObserver, roles map[string]hlsWallPauseOwnedJob, started, paused time.Time, phase string) hlsWallPauseSample {
	t.Helper()
	sample := hlsWallPauseSample{Phase: phase, ElapsedMS: time.Since(started).Milliseconds()}
	if !paused.IsZero() {
		sample.PauseElapsedMS = time.Since(paused).Milliseconds()
	}
	metrics, ok := h.f.app.hls.manager.(interface{ Metrics() transcode.Metrics })
	if !ok {
		t.Fatal("real manager has no slot observation API")
	}
	sample.Slots = metrics.Metrics()
	resources, ok := h.f.app.hls.manager.(interface {
		ResourceUsage(context.Context, transcode.Scope) (transcode.ResourceUsage, error)
	})
	if !ok {
		t.Fatal("real manager has no readonly scoped resource observation API")
	}
	scopes := make(map[transcode.Scope]string)
	for _, owned := range roles {
		role := owned.Role
		if owned.Scope == graph.session.key.scope {
			role = "target"
		}
		scopes[owned.Scope] = role
	}
	for scope, role := range scopes {
		usage, resourceErr := resources.ResourceUsage(h.f.ctx, scope)
		if resourceErr != nil {
			t.Fatalf("read owned scoped manager resources: error_type=%T", resourceErr)
		}
		sample.ManagerScopes = append(sample.ManagerScopes, hlsWallPauseScopedUsage{Role: role, PlaybackID: scope.PlaySessionID, Usage: usage})
	}
	sort.Slice(sample.ManagerScopes, func(i, j int) bool { return sample.ManagerScopes[i].PlaybackID < sample.ManagerScopes[j].PlaybackID })
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(os.Getpid()), "stat"))
	start, _, parseErr := hlsWallPauseStat(stat)
	if err != nil || parseErr != nil {
		t.Fatal("server process identity is unavailable")
	}
	sample.ServerCounters, err = hlsWallPauseReadProcess(os.Getpid(), start)
	if err != nil {
		t.Fatalf("read server process counters: error_type=%T", err)
	}
	graph.session.mu.Lock()
	sample.TargetPaused, sample.TargetClosed = graph.session.demand.paused, graph.session.closed
	if graph.session.windowGraph != nil {
		for _, binding := range graph.session.windowGraph.slots {
			for _, pin := range binding.redirectPins {
				if pin != nil {
					sample.TargetPins++
				}
			}
		}
	}
	graph.session.mu.Unlock()
	h.f.app.hls.generatedWindowPinMu.Lock()
	sample.AllPins = len(h.f.app.hls.generatedWindowPinOwners)
	h.f.app.hls.generatedWindowPinMu.Unlock()
	fdEntries, err := os.ReadDir(filepath.Join("/proc", strconv.Itoa(os.Getpid()), "fd"))
	if err != nil {
		t.Fatalf("read server descriptor inventory: error_type=%T", err)
	}
	fdPaths := make([]string, 0, len(fdEntries))
	for _, entry := range fdEntries {
		if path, linkErr := os.Readlink(filepath.Join("/proc", strconv.Itoa(os.Getpid()), "fd", entry.Name())); linkErr == nil {
			fdPaths = append(fdPaths, strings.TrimSuffix(path, " (deleted)"))
		}
	}
	ids := make([]string, 0, len(roles))
	for id := range roles {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		job := hlsWallPauseJobSample{ID: id, Role: roles[id].Role, State: "not_retained"}
		record, snapshotErr := h.f.app.hls.manager.Snapshot(roles[id].Scope, id)
		if record.ID == id {
			job.State, job.ErrorCode, job.ChargedBytes = record.State, record.ErrorCode, record.OutputBytes
			job.LastAccessUTC = record.LastAccessAt.UTC().Format(time.RFC3339Nano)
			if record.State == "queued" {
				sample.QueuedRecords++
			}
			sample.ChargedRecords += record.OutputBytes
		} else if errors.Is(snapshotErr, transcode.ErrManagerClosed) {
			job.State = "manager_closed"
		} else if !errors.Is(snapshotErr, transcode.ErrJobNotFound) {
			t.Fatalf("read owned manager snapshot: error_type=%T", snapshotErr)
		}
		jobDirectory := filepath.Join(h.f.app.cfg.Transcoding.CacheDirectory, id)
		entries, readErr := os.ReadDir(jobDirectory)
		job.Directory = readErr == nil
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			t.Fatalf("read owned cache directory: error_type=%T", readErr)
		}
		for _, entry := range entries {
			if entry.IsDir() || !(strings.HasSuffix(entry.Name(), ".ts") || strings.HasSuffix(entry.Name(), ".m3u8")) {
				continue
			}
			info, infoErr := entry.Info()
			if errors.Is(infoErr, os.ErrNotExist) {
				// Maintenance can lawfully remove a directory between inventory
				// and stat. Observation must neither pin it nor refresh its lease.
				continue
			}
			if infoErr != nil {
				t.Fatalf("read owned published file facts: error_type=%T", infoErr)
			}
			job.Published = append(job.Published, hlsWallPauseFile{Name: entry.Name(), Bytes: info.Size()})
		}
		for _, path := range fdPaths {
			if path == jobDirectory || strings.HasPrefix(path, jobDirectory+string(filepath.Separator)) {
				job.ServerFDs++
			}
		}
		sample.Jobs = append(sample.Jobs, job)
	}
	sample.Processes, _ = observer.snapshot()
	sample.CaptureEndMS = time.Since(started).Milliseconds()
	return sample
}

func TestHLSGeneratedWindowWallPauseStatAndOwnedIDParsing(t *testing.T) {
	// proc names may contain spaces and parentheses; only the final delimiter
	// begins the numeric fields. Reject truncated evidence and unsafe job IDs.
	fields := make([]string, 20)
	for index := range fields {
		fields[index] = "0"
	}
	fields[0], fields[11], fields[12], fields[19] = "R", "7", "9", "31"
	start, counters, err := hlsWallPauseStat([]byte("123 (encoder ) worker) " + strings.Join(fields, " ")))
	if err != nil || start != 31 || counters.UserTicks != 7 || counters.SystemTicks != 9 {
		t.Fatal("stat parser lost the original process identity or CPU counters")
	}
	if _, _, err := hlsWallPauseStat([]byte("123 (encoder) R 1")); err == nil {
		t.Fatal("truncated process evidence was accepted")
	}
	if !hlsWallPauseJobID(strings.Repeat("a", 32)) || hlsWallPauseJobID("../"+strings.Repeat("a", 32)) || hlsWallPauseJobID(strings.Repeat("A", 32)) {
		t.Fatal("owned job ID guard accepted an unsafe identity")
	}
}
