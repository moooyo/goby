package transcode

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrInvalidOptions    = errors.New("invalid transcode manager options")
	ErrInvalidScope      = errors.New("invalid transcode scope")
	ErrManagerClosed     = errors.New("transcode manager is closed")
	ErrBusy              = errors.New("transcode capacity is exhausted")
	ErrQuota             = errors.New("transcode cache quota is exhausted")
	ErrJobNotFound       = errors.New("transcode job was not found")
	ErrJobFailed         = errors.New("transcode job failed")
	ErrJobCancelled      = errors.New("transcode job was cancelled")
	ErrOutputUnavailable = errors.New("transcode output is unavailable")
	ErrPersistence       = errors.New("transcode status could not be persisted")
)

type runnerFunc func(context.Context, string, string, *os.File, Plan, int, func(Progress)) (RunResult, error)

// Options bounds process concurrency, queued source descriptors, retained job
// metadata, and cache storage. Zero values select defaults. Storage limits are
// checked before admission and periodically during execution; a process can
// write additional bytes between checks. The cache must be a dedicated local
// Linux directory, and Repository must persist records in the catalog database.
type Options struct {
	Root              string
	FFmpegPath        string
	Threads           int
	MaxJobs           int
	MaxUserJobs       int
	MaxSessionJobs    int
	MaxQueueJobs      int
	MaxRetainedJobs   int
	MaxReaders        int
	MaxJobReaders     int
	MaxBytes          int64
	MaxJobBytes       int64
	MinFreeBytes      int64
	IdleTimeout       time.Duration
	StartupTimeout    time.Duration
	NoProgressTimeout time.Duration
	MaxRuntime        time.Duration
	Repository        Repository
	run               runnerFunc
	pollInterval      time.Duration
}

type managedJob struct {
	record       Record
	input        *os.File
	ctx          context.Context
	cancel       context.CancelFunc
	created      chan struct{}
	launch       chan struct{}
	done         chan struct{}
	changed      chan struct{}
	durable      bool
	running      bool
	finished     bool
	directory    bool
	ready        bool
	readers      int
	stopCode     string
	started      time.Time
	lastProgress time.Time
	outputTicks  int64
	progressSize int64
}

// Manager owns every input accepted by Ensure and every process it starts.
// Authorization belongs to the caller: each lookup additionally requires an
// exact scope match, and no original authentication token is retained here.
type Manager struct {
	options      Options
	cache        *cacheRoot
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.Mutex
	filesMu      sync.Mutex
	jobs         map[string]*managedJob
	bySpec       map[Spec]*managedJob
	queue        []*managedJob
	running      int
	runningUsers map[string]int
	runningAuth  map[string]int
	bytes        int64
	readers      int
	cacheFailed  bool
	closing      bool
	closeErr     error
	wake         chan struct{}
	loopDone     chan struct{}
	closed       chan struct{}
	workers      sync.WaitGroup
}

// NewManager recovers only an exclusively locked, explicitly owned cache. The
// caller must also hold exclusive catalog ownership before invoking recovery;
// a filesystem lock cannot exclude another cache root using the same database.
// startupCtx limits initialization; it never becomes the lifetime context of
// jobs or the maintenance worker.
func NewManager(startupCtx context.Context, options Options) (*Manager, error) {
	options, err := normalizeManagerOptions(options)
	if err != nil {
		return nil, err
	}
	if err := startupCtx.Err(); err != nil {
		return nil, err
	}
	cache, err := openCacheRoot(options.Root)
	if err != nil {
		return nil, err
	}
	if err = cache.Recover(); err != nil {
		_ = cache.Close()
		return nil, err
	}
	recoveryCtx, cancelRecovery := context.WithTimeout(startupCtx, 10*time.Second)
	err = options.Repository.Recover(recoveryCtx)
	cancelRecovery()
	if err != nil {
		_ = cache.Close()
		return nil, fmt.Errorf("%w: recover jobs", ErrPersistence)
	}
	if err = startupCtx.Err(); err != nil {
		_ = cache.Close()
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{options: options, cache: cache, ctx: ctx, cancel: cancel,
		jobs: make(map[string]*managedJob), bySpec: make(map[Spec]*managedJob),
		runningUsers: make(map[string]int), runningAuth: make(map[string]int),
		wake: make(chan struct{}, 1), loopDone: make(chan struct{}), closed: make(chan struct{})}
	go m.loop()
	return m, nil
}

func normalizeManagerOptions(o Options) (Options, error) {
	if o.Root == "" || !filepath.IsAbs(o.Root) || o.FFmpegPath == "" || strings.ContainsAny(o.FFmpegPath, "\x00\r\n") || o.Repository == nil {
		return o, ErrInvalidOptions
	}
	if o.Threads == 0 {
		o.Threads = 2
	}
	if o.MaxJobs == 0 {
		o.MaxJobs = 2
	}
	if o.MaxUserJobs == 0 {
		o.MaxUserJobs = 1
	}
	if o.MaxSessionJobs == 0 {
		o.MaxSessionJobs = 1
	}
	if o.MaxQueueJobs == 0 {
		o.MaxQueueJobs = 16
	}
	if o.MaxRetainedJobs == 0 {
		o.MaxRetainedJobs = 128
	}
	if o.MaxReaders == 0 {
		o.MaxReaders = 256
	}
	if o.MaxJobReaders == 0 {
		o.MaxJobReaders = min(32, o.MaxReaders)
	}
	if o.MaxBytes == 0 {
		o.MaxBytes = 20 << 30
	}
	if o.MaxJobBytes == 0 {
		o.MaxJobBytes = 8 << 30
	}
	if o.MinFreeBytes == 0 {
		o.MinFreeBytes = 512 << 20
	}
	if o.IdleTimeout == 0 {
		o.IdleTimeout = 2 * time.Minute
	}
	if o.StartupTimeout == 0 {
		o.StartupTimeout = 30 * time.Second
	}
	if o.NoProgressTimeout == 0 {
		o.NoProgressTimeout = 30 * time.Second
	}
	if o.MaxRuntime == 0 {
		o.MaxRuntime = 4 * time.Hour
	}
	if o.pollInterval == 0 {
		o.pollInterval = 250 * time.Millisecond
	}
	if o.run == nil {
		o.run = Run
	}
	if o.Threads < 1 || o.Threads > 64 || o.MaxJobs < 1 || o.MaxJobs > 64 ||
		o.MaxUserJobs < 1 || o.MaxUserJobs > o.MaxJobs || o.MaxSessionJobs < 1 || o.MaxSessionJobs > o.MaxJobs ||
		o.MaxQueueJobs < 1 || o.MaxQueueJobs > 1024 || o.MaxRetainedJobs < o.MaxJobs || o.MaxRetainedJobs > 4096 ||
		o.MaxReaders < 1 || o.MaxReaders > 4096 || o.MaxJobReaders < 1 || o.MaxJobReaders > o.MaxReaders ||
		o.MaxBytes < 1 || o.MaxJobBytes < 1 || o.MaxJobBytes > o.MaxBytes || o.MinFreeBytes < 0 ||
		o.IdleTimeout < time.Millisecond || o.StartupTimeout < time.Millisecond || o.NoProgressTimeout < time.Millisecond ||
		o.MaxRuntime < time.Millisecond || o.pollInterval < time.Millisecond || o.pollInterval > time.Second {
		return o, ErrInvalidOptions
	}
	return o, nil
}

func validScope(scope Scope) bool {
	for _, value := range []string{scope.UserID, scope.AuthSessionID, scope.PlaySessionID, scope.ItemID, scope.SourceID} {
		if !validManagerIdentifier(value, 256, false) {
			return false
		}
	}
	return validManagerIdentifier(scope.DeviceID, 256, true)
}

func validManagerIdentifier(value string, limit int, optional bool) bool {
	return (optional || value != "") && len(value) <= limit && utf8.ValidString(value) &&
		strings.TrimSpace(value) == value && strings.IndexFunc(value, unicode.IsControl) < 0
}

// Ensure always consumes input, including on invalid arguments, duplicate jobs,
// exhausted capacity, and persistence errors. An identical live/completed spec
// reuses its job; a failed or cancelled job can be retried with a new ID.
func (m *Manager) Ensure(ctx context.Context, spec Spec, input *os.File) (Record, error) {
	owned := false
	defer func() {
		if !owned && input != nil {
			_ = input.Close()
		}
	}()
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	if !validScope(spec.Scope) || !validManagerIdentifier(spec.SourceStamp, 256, false) {
		return Record{}, ErrInvalidScope
	}
	if err := ValidatePlan(spec.Plan); err != nil {
		return Record{}, err
	}
	if input == nil {
		return Record{}, ErrInvalidInput
	}
	info, err := input.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return Record{}, ErrInvalidInput
	}
	m.mu.Lock()
	if m.closing {
		m.mu.Unlock()
		return Record{}, ErrManagerClosed
	}
	if m.cacheFailed {
		m.mu.Unlock()
		return Record{}, ErrOutputUnavailable
	}
	if existing := m.bySpec[spec]; existing != nil && existing.stopCode == "" {
		m.touchLocked(existing)
		record, created := existing.record, existing.created
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return Record{}, ctx.Err()
		case <-created:
		}
		m.mu.Lock()
		record = existing.record
		switch {
		case m.closing:
			err = ErrManagerClosed
		case m.cacheFailed:
			err = ErrOutputUnavailable
		case m.jobs[record.ID] != existing:
			err = ErrJobNotFound
		default:
			err = jobError(existing)
		}
		m.mu.Unlock()
		return record, err
	}
	if len(m.jobs) >= m.options.MaxRetainedJobs || m.queuedLocked() >= m.options.MaxQueueJobs {
		m.mu.Unlock()
		return Record{}, ErrBusy
	}
	if m.bytes >= m.options.MaxBytes {
		m.mu.Unlock()
		return Record{}, ErrQuota
	}
	m.mu.Unlock()
	m.filesMu.Lock()
	free, err := m.cache.FreeBytes()
	m.filesMu.Unlock()
	if err != nil {
		return Record{}, ErrOutputUnavailable
	}
	if free < m.options.MinFreeBytes {
		return Record{}, ErrQuota
	}
	var randomID [16]byte
	if _, err = rand.Read(randomID[:]); err != nil {
		return Record{}, ErrBusy
	}
	now := time.Now().UTC()
	jobCtx, cancel := context.WithCancel(m.ctx)
	j := &managedJob{record: Record{ID: hex.EncodeToString(randomID[:]), Spec: spec, State: "queued", CreatedAt: now, UpdatedAt: now, LastAccessAt: now},
		input: input, ctx: jobCtx, cancel: cancel, created: make(chan struct{}), launch: make(chan struct{}), done: make(chan struct{}), changed: make(chan struct{})}
	m.mu.Lock()
	if m.closing {
		m.mu.Unlock()
		cancel()
		return Record{}, ErrManagerClosed
	}
	if m.cacheFailed {
		m.mu.Unlock()
		cancel()
		return Record{}, ErrOutputUnavailable
	}
	// Admission is repeated after the filesystem call: another request may have
	// inserted the same spec or filled the queue while that call was in flight.
	if existing := m.bySpec[spec]; existing != nil && existing.stopCode == "" {
		m.mu.Unlock()
		cancel()
		owned = true
		return m.Ensure(ctx, spec, input)
	}
	if len(m.jobs) >= m.options.MaxRetainedJobs || m.queuedLocked() >= m.options.MaxQueueJobs {
		m.mu.Unlock()
		cancel()
		return Record{}, ErrBusy
	}
	if m.bytes >= m.options.MaxBytes {
		m.mu.Unlock()
		cancel()
		return Record{}, ErrQuota
	}
	m.jobs[j.record.ID], m.bySpec[spec] = j, j
	m.queue = append(m.queue, j)
	initialRecord := j.record
	m.workers.Add(1)
	owned = true
	m.mu.Unlock()
	go m.runJob(j)
	createCtx, cancelCreate := context.WithTimeout(ctx, 5*time.Second)
	stopCreate := context.AfterFunc(m.ctx, cancelCreate)
	err = m.options.Repository.Create(createCtx, initialRecord)
	stopCreate()
	cancelCreate()
	m.mu.Lock()
	if err != nil {
		j.stopCode = "persistence"
		j.cancel()
	} else {
		j.durable = true
		if ctx.Err() != nil {
			m.stopLocked(j, "cancelled")
		}
	}
	close(j.created)
	m.notifyLocked(j)
	record := j.record
	closing := m.closing
	m.mu.Unlock()
	m.signal()
	if err != nil {
		return record, ErrPersistence
	}
	if err = ctx.Err(); err != nil {
		return record, err
	}
	if closing {
		return record, ErrManagerClosed
	}
	return record, nil
}

func (m *Manager) queuedLocked() int {
	count := 0
	for _, j := range m.jobs {
		if !j.running && !j.finished && j.stopCode == "" {
			count++
		}
	}
	return count
}

func (m *Manager) touchLocked(j *managedJob) { j.record.LastAccessAt = time.Now().UTC() }

func (m *Manager) notifyLocked(j *managedJob) {
	close(j.changed)
	j.changed = make(chan struct{})
}

func (m *Manager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Manager) stopLocked(j *managedJob, code string) {
	if j.finished || j.stopCode != "" {
		return
	}
	j.stopCode = code
	if m.bySpec[j.record.Spec] == j {
		delete(m.bySpec, j.record.Spec)
	}
	j.cancel()
	m.notifyLocked(j)
}

func jobError(j *managedJob) error {
	if j.stopCode != "" || j.record.State == "failed" || j.record.State == "cancelled" || j.record.State == "interrupted" {
		code := j.stopCode
		if code == "" {
			code = j.record.ErrorCode
		}
		switch code {
		case "cancelled", "session_cancelled", "idle_timeout", "manager_closed":
			return ErrJobCancelled
		case "cache_quota", "job_quota", "cache_space":
			return ErrQuota
		case "persistence":
			return ErrPersistence
		default:
			return ErrJobFailed
		}
	}
	return nil
}

func (m *Manager) lookupLocked(scope Scope, id string) (*managedJob, error) {
	if m.closing {
		return nil, ErrManagerClosed
	}
	if m.cacheFailed {
		return nil, ErrOutputUnavailable
	}
	j := m.jobs[id]
	if j == nil || j.record.Spec.Scope != scope {
		return nil, ErrJobNotFound
	}
	return j, nil
}

// Snapshot returns the current in-memory status without refreshing its idle
// lease. A stopped or failed job returns both its record and its classified
// error. A completed job can therefore retain State "completed" while returning
// ErrJobCancelled after its cached output has been explicitly invalidated.
func (m *Manager) Snapshot(scope Scope, id string) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, err := m.lookupLocked(scope, id)
	if err != nil {
		return Record{}, err
	}
	return j.record, jobError(j)
}

// WaitReady waits for an atomically published playlist and at least one
// completed segment. It does not expose FFmpeg temporary output files.
func (m *Manager) WaitReady(ctx context.Context, scope Scope, id string) (Record, error) {
	for {
		if err := ctx.Err(); err != nil {
			return Record{}, err
		}
		m.mu.Lock()
		j, err := m.lookupLocked(scope, id)
		if err != nil {
			m.mu.Unlock()
			return Record{}, err
		}
		m.touchLocked(j)
		record, changed := j.record, j.changed
		err = jobError(j)
		ready := j.ready
		m.mu.Unlock()
		if err != nil {
			return record, err
		}
		if ready {
			return record, nil
		}
		select {
		case <-ctx.Done():
			return Record{}, ctx.Err()
		case <-changed:
		}
	}
}

// ReadHandle pins its job's cache files until Close. Callers must use this Close
// method rather than closing the embedded file directly.
type ReadHandle struct {
	*os.File
	once    sync.Once
	release func()
	err     error
}

func (h *ReadHandle) Close() error {
	h.once.Do(func() { h.err = h.File.Close(); h.release() })
	return h.err
}

// Open requires a complete ownership scope. The API/catalog layer must
// revalidate the current token, user policy, source, and library permission
// before each call. Only final HLS output names can be opened.
func (m *Manager) Open(ctx context.Context, scope Scope, id, name string) (*ReadHandle, error) {
	if !validOutputName(name) {
		return nil, ErrOutputUnavailable
	}
	if _, err := m.WaitReady(ctx, scope, id); err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		attempt, err := m.tryOpen(scope, id, name)
		if err == nil {
			if err := ctx.Err(); err != nil {
				_ = attempt.handle.Close()
				return nil, err
			}
			return attempt.handle, nil
		}
		if !attempt.pending || !errors.Is(err, ErrOutputUnavailable) {
			return nil, err
		}
		timer := time.NewTimer(m.options.pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-attempt.changed:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// TryOpen performs one output lookup without waiting for startup or a future
// segment. It returns ErrOutputUnavailable until the job is ready and the final
// requested file exists. Filesystem access and manager locks still synchronize
// normally; this method performs no database authorization. The caller must
// revalidate its current token, user policy, source, and library permission.
func (m *Manager) TryOpen(scope Scope, id, name string) (*ReadHandle, error) {
	attempt, err := m.tryOpen(scope, id, name)
	return attempt.handle, err
}

type outputAttempt struct {
	handle  *ReadHandle
	changed <-chan struct{}
	pending bool
}

func (m *Manager) tryOpen(scope Scope, id, name string) (outputAttempt, error) {
	var attempt outputAttempt
	if !validOutputName(name) {
		return attempt, ErrOutputUnavailable
	}
	m.mu.Lock()
	j, err := m.lookupLocked(scope, id)
	if err == nil {
		err = jobError(j)
	}
	if err != nil {
		m.mu.Unlock()
		return attempt, err
	}
	m.touchLocked(j)
	attempt.changed = j.changed
	if !j.ready {
		attempt.pending = !j.finished
		m.mu.Unlock()
		return attempt, ErrOutputUnavailable
	}
	if m.readers >= m.options.MaxReaders || j.readers >= m.options.MaxJobReaders {
		m.mu.Unlock()
		return attempt, ErrBusy
	}
	j.readers++
	m.readers++
	m.mu.Unlock()
	m.filesMu.Lock()
	file, openErr := m.cache.OpenJobFile(id, name)
	m.filesMu.Unlock()
	if openErr == nil {
		info, statErr := file.Stat()
		if statErr != nil || info.Size() <= 0 || info.Size() > m.options.MaxJobBytes || (name == "main.m3u8" && info.Size() > 1<<20) {
			openErr = ErrOutputUnavailable
		}
	}
	// Pinning prevents reclamation during the filesystem operation. Recheck
	// cancellation and shutdown before publishing the newly opened handle.
	m.mu.Lock()
	current, err := m.lookupLocked(scope, id)
	if err == nil && current != j {
		err = ErrJobNotFound
	}
	if err == nil {
		err = jobError(j)
	}
	if err == nil {
		attempt.changed = j.changed
		if !j.ready {
			attempt.pending = !j.finished
			err = ErrOutputUnavailable
		} else if openErr != nil {
			attempt.pending = errors.Is(openErr, os.ErrNotExist) && !j.finished
			err = ErrOutputUnavailable
		}
	}
	m.mu.Unlock()
	if err != nil {
		if file != nil {
			_ = file.Close()
		}
		m.releaseReader(j)
		return attempt, err
	}
	attempt.handle = &ReadHandle{File: file, release: func() { m.releaseReader(j) }}
	return attempt, nil
}

func (m *Manager) releaseReader(j *managedJob) {
	m.mu.Lock()
	j.readers--
	m.readers--
	m.touchLocked(j)
	m.mu.Unlock()
	m.signal()
}

// Cancel stops exactly the supplied full scope and waits for its processes and
// input descriptors to be released. A caller deadline does not undo cancellation.
func (m *Manager) Cancel(ctx context.Context, scope Scope) error {
	if !validScope(scope) {
		return ErrInvalidScope
	}
	m.mu.Lock()
	var pending []<-chan struct{}
	for _, j := range m.jobs {
		if j.record.Spec.Scope == scope {
			if j.finished {
				m.invalidateFinishedLocked(j, "cancelled")
			} else {
				m.stopLocked(j, "cancelled")
			}
			pending = append(pending, j.done)
		}
	}
	m.mu.Unlock()
	m.signal()
	for _, done := range pending {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-done:
		}
	}
	return nil
}

// CancelJob invalidates exactly one job without waiting for its process to
// exit. Other jobs with the same scope, including a replacement seek producer,
// remain usable. Completed jobs retain their durable completion history, while
// their cached files are reclaimed after all already-open readers close.
func (m *Manager) CancelJob(id string, scope Scope) error {
	if !validScope(scope) {
		return ErrInvalidScope
	}
	m.mu.Lock()
	j, err := m.lookupLocked(scope, id)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	if j.finished {
		m.invalidateFinishedLocked(j, "cancelled")
	} else {
		m.stopLocked(j, "cancelled")
	}
	m.mu.Unlock()
	m.signal()
	return nil
}

// CancelSession immediately prevents any further lookup of an authenticated
// session's work. Process termination and cache reclamation proceed in the
// background, so a logout response does not wait for FFmpeg's termination grace.
func (m *Manager) CancelSession(authID string) {
	if authID == "" {
		return
	}
	m.mu.Lock()
	for _, j := range m.jobs {
		if j.record.Spec.Scope.AuthSessionID == authID {
			if j.finished {
				m.invalidateFinishedLocked(j, "session_cancelled")
			} else {
				m.stopLocked(j, "session_cancelled")
			}
		}
	}
	m.mu.Unlock()
	m.signal()
}

func (m *Manager) invalidateFinishedLocked(j *managedJob, code string) {
	if j.stopCode == "" {
		j.stopCode = code
	}
	if m.bySpec[j.record.Spec] == j {
		delete(m.bySpec, j.record.Spec)
	}
	m.notifyLocked(j)
}

func (m *Manager) loop() {
	defer close(m.loopDone)
	ticker := time.NewTicker(m.options.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.wake:
		case <-ticker.C:
		}
		m.maintain()
		m.schedule()
	}
}

func (m *Manager) schedule() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closing || m.cacheFailed {
		return
	}
	remaining := m.queue[:0]
	for _, j := range m.queue {
		if j.finished || j.stopCode != "" {
			continue
		}
		if !j.durable || m.running >= m.options.MaxJobs || m.runningUsers[j.record.Spec.Scope.UserID] >= m.options.MaxUserJobs ||
			m.runningAuth[j.record.Spec.Scope.AuthSessionID] >= m.options.MaxSessionJobs || m.bytes >= m.options.MaxBytes {
			remaining = append(remaining, j)
			continue
		}
		j.running = true
		j.started = time.Now().UTC()
		j.lastProgress = j.started
		j.record.State, j.record.UpdatedAt = "running", j.started
		m.running++
		m.runningUsers[j.record.Spec.Scope.UserID]++
		m.runningAuth[j.record.Spec.Scope.AuthSessionID]++
		close(j.launch)
		m.notifyLocked(j)
	}
	for i := len(remaining); i < len(m.queue); i++ {
		m.queue[i] = nil
	}
	m.queue = remaining
}

func (m *Manager) runJob(j *managedJob) {
	defer m.workers.Done()
	<-j.created
	select {
	case <-j.ctx.Done():
		m.finish(j, j.ctx.Err())
		return
	case <-j.launch:
	}
	if err := m.persist(j); err != nil {
		m.fail(j, "persistence")
		m.finish(j, err)
		return
	}
	if err := j.ctx.Err(); err != nil {
		m.finish(j, err)
		return
	}
	m.filesMu.Lock()
	free, err := m.cache.FreeBytes()
	if err == nil && free < m.options.MinFreeBytes {
		err = ErrQuota
	}
	if err == nil {
		err = m.cache.CreateJob(j.record.ID)
	}
	m.filesMu.Unlock()
	if err != nil {
		if errors.Is(err, ErrQuota) {
			m.fail(j, "cache_space")
		} else {
			m.fail(j, "cache_unavailable")
		}
		m.finish(j, err)
		return
	}
	m.mu.Lock()
	j.directory = true
	m.mu.Unlock()
	m.filesMu.Lock()
	directory, err := m.cache.JobPath(j.record.ID)
	m.filesMu.Unlock()
	if err != nil {
		m.fail(j, "cache_unavailable")
		m.finish(j, err)
		return
	}
	_, err = m.options.run(j.ctx, m.options.FFmpegPath, directory, j.input, j.record.Spec.Plan, m.options.Threads, func(p Progress) {
		m.mu.Lock()
		if p.OutputTicks > j.outputTicks || p.Bytes > j.progressSize {
			j.lastProgress = time.Now().UTC()
			if p.OutputTicks > j.outputTicks {
				j.outputTicks = p.OutputTicks
			}
			if p.Bytes > j.progressSize {
				j.progressSize = p.Bytes
			}
		}
		m.mu.Unlock()
	})
	m.finish(j, err)
}

func (m *Manager) fail(j *managedJob, code string) { m.mu.Lock(); m.stopLocked(j, code); m.mu.Unlock() }

func (m *Manager) persist(j *managedJob) error {
	m.mu.Lock()
	durable, record := j.durable, j.record
	m.mu.Unlock()
	if !durable {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.options.Repository.Update(ctx, record); err != nil {
		return ErrPersistence
	}
	return nil
}

func (m *Manager) finish(j *managedJob, runErr error) {
	_ = j.input.Close()
	m.mu.Lock()
	directory := j.directory
	m.mu.Unlock()
	var size int64
	var ready bool
	var scanErr error
	if directory {
		m.filesMu.Lock()
		size, ready, scanErr = m.cache.ScanJob(j.record.ID)
	}
	m.mu.Lock()
	m.bytes += size - j.record.OutputBytes
	j.record.OutputBytes = size
	if j.stopCode == "" {
		switch {
		case size > m.options.MaxJobBytes:
			j.stopCode = "job_quota"
		case m.bytes > m.options.MaxBytes:
			j.stopCode = "cache_quota"
		case runErr != nil:
			j.stopCode = runnerErrorCode(runErr)
		case scanErr != nil || !ready:
			j.stopCode = "invalid_output"
		}
	}
	if j.stopCode == "" {
		j.record.State, j.ready = "completed", true
	} else {
		j.record.State = "failed"
		if j.stopCode == "cancelled" || j.stopCode == "session_cancelled" || j.stopCode == "idle_timeout" || j.stopCode == "manager_closed" {
			j.record.State = "cancelled"
		}
		j.record.ErrorCode = j.stopCode
		if m.bySpec[j.record.Spec] == j {
			delete(m.bySpec, j.record.Spec)
		}
	}
	j.record.UpdatedAt = time.Now().UTC()
	if j.running {
		j.running = false
		m.running--
		m.runningUsers[j.record.Spec.Scope.UserID]--
		m.runningAuth[j.record.Spec.Scope.AuthSessionID]--
		if m.runningUsers[j.record.Spec.Scope.UserID] == 0 {
			delete(m.runningUsers, j.record.Spec.Scope.UserID)
		}
		if m.runningAuth[j.record.Spec.Scope.AuthSessionID] == 0 {
			delete(m.runningAuth, j.record.Spec.Scope.AuthSessionID)
		}
	}
	m.mu.Unlock()
	if directory {
		m.filesMu.Unlock()
	}
	err := m.persist(j)
	m.mu.Lock()
	if err != nil {
		j.stopCode, j.record.ErrorCode, j.record.State = "persistence", "persistence", "failed"
		if m.bySpec[j.record.Spec] == j {
			delete(m.bySpec, j.record.Spec)
		}
	}
	j.finished = true
	close(j.done)
	m.notifyLocked(j)
	m.mu.Unlock()
	j.cancel()
	m.signal()
}

func runnerErrorCode(err error) string {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "cancelled"
	case errors.Is(err, ErrStart):
		return "process_start"
	case errors.Is(err, ErrProgress):
		return "process_progress"
	case errors.Is(err, ErrUnsupported):
		return "unsupported"
	case errors.Is(err, ErrInvalidInput):
		return "source_unavailable"
	case errors.Is(err, ErrInvalidDirectory):
		return "cache_unavailable"
	default:
		return "process_failed"
	}
}

func (m *Manager) maintain() {
	m.mu.Lock()
	jobs := make([]*managedJob, 0, len(m.jobs))
	for _, j := range m.jobs {
		jobs = append(jobs, j)
	}
	m.mu.Unlock()
	for _, j := range jobs {
		m.mu.Lock()
		directory, scan := j.directory, j.directory && (j.running || j.record.State == "completed")
		m.mu.Unlock()
		if !directory || !scan {
			continue
		}
		m.filesMu.Lock()
		size, ready, err := m.cache.ScanJob(j.record.ID)
		m.mu.Lock()
		if err != nil {
			if !j.finished {
				m.stopLocked(j, "cache_unavailable")
			} else {
				m.invalidateFinishedLocked(j, "cache_unavailable")
			}
		} else {
			changed := size != j.record.OutputBytes || ready != j.ready
			if size > j.record.OutputBytes && j.running {
				j.lastProgress = time.Now().UTC()
			}
			m.bytes += size - j.record.OutputBytes
			j.record.OutputBytes = size
			j.ready = ready
			if size > m.options.MaxJobBytes {
				if j.finished {
					m.invalidateFinishedLocked(j, "job_quota")
				} else {
					m.stopLocked(j, "job_quota")
				}
			}
			if changed {
				m.notifyLocked(j)
			}
		}
		m.mu.Unlock()
		m.filesMu.Unlock()
	}
	m.filesMu.Lock()
	free, freeErr := m.cache.FreeBytes()
	m.filesMu.Unlock()
	now := time.Now().UTC()
	m.mu.Lock()
	for _, j := range m.jobs {
		if j.finished {
			continue
		}
		switch {
		case m.cacheFailed:
			m.stopLocked(j, "cache_unavailable")
		case freeErr != nil:
			m.stopLocked(j, "cache_unavailable")
		case free < m.options.MinFreeBytes:
			m.stopLocked(j, "cache_space")
		case m.bytes > m.options.MaxBytes:
			m.stopLocked(j, "cache_quota")
		case j.readers == 0 && now.Sub(j.record.LastAccessAt) > m.options.IdleTimeout:
			m.stopLocked(j, "idle_timeout")
		case j.running && now.Sub(j.started) > m.options.MaxRuntime:
			m.stopLocked(j, "runtime_timeout")
		case j.running && !j.ready && now.Sub(j.started) > m.options.StartupTimeout:
			m.stopLocked(j, "startup_timeout")
		case j.running && now.Sub(j.lastProgress) > m.options.NoProgressTimeout:
			m.stopLocked(j, "progress_timeout")
		}
	}
	m.mu.Unlock()
	for _, j := range jobs {
		m.reclaim(j, false)
	}
}

func (m *Manager) reclaim(j *managedJob, closing bool) bool {
	m.mu.Lock()
	if !j.finished || j.readers != 0 {
		m.mu.Unlock()
		return false
	}
	expired := closing || time.Since(j.record.LastAccessAt) >= m.options.IdleTimeout
	removeFiles := j.directory && (expired || j.stopCode != "")
	if removeFiles {
		j.directory = false
	}
	if expired {
		delete(m.jobs, j.record.ID)
		if m.bySpec[j.record.Spec] == j {
			delete(m.bySpec, j.record.Spec)
		}
	}
	m.mu.Unlock()
	if removeFiles {
		m.filesMu.Lock()
		err := m.cache.RemoveJob(j.record.ID)
		m.filesMu.Unlock()
		m.mu.Lock()
		if err == nil {
			m.bytes -= j.record.OutputBytes
			j.record.OutputBytes = 0
		} else {
			// Unsafe content remains untouched. The manager stops admitting work
			// until the owner resolves it and explicitly restarts the service.
			j.stopCode = "cache_unavailable"
			m.cacheFailed = true
			m.closeErr = errors.Join(m.closeErr, err)
		}
		m.mu.Unlock()
	}
	return expired
}

// Close starts an irreversible background shutdown. Even when ctx expires, the
// manager continues reaping children and waits for outstanding readers before
// removing cache files and releasing its filesystem lock. It is safe to retry.
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	if !m.closing {
		m.closing = true
		for _, j := range m.jobs {
			if !j.finished {
				m.stopLocked(j, "manager_closed")
			}
		}
		m.cancel()
		go m.shutdown()
	}
	m.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-m.closed:
		m.mu.Lock()
		err := m.closeErr
		m.mu.Unlock()
		return err
	}
}

func (m *Manager) shutdown() {
	<-m.loopDone
	m.workers.Wait()
	ticker := time.NewTicker(m.options.pollInterval)
	defer ticker.Stop()
	for {
		m.mu.Lock()
		jobs := make([]*managedJob, 0, len(m.jobs))
		for _, j := range m.jobs {
			jobs = append(jobs, j)
		}
		m.mu.Unlock()
		if len(jobs) == 0 {
			break
		}
		for _, j := range jobs {
			m.reclaim(j, true)
		}
		m.mu.Lock()
		remaining := len(m.jobs)
		m.mu.Unlock()
		if remaining == 0 {
			break
		}
		select {
		case <-m.wake:
		case <-ticker.C:
		}
	}
	m.filesMu.Lock()
	err := m.cache.Close()
	m.filesMu.Unlock()
	m.mu.Lock()
	m.closeErr = errors.Join(m.closeErr, err)
	close(m.closed)
	m.mu.Unlock()
}
