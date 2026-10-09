package transcode

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/media"
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

// Options bounds process concurrency, accepted source descriptors, retained job
// metadata, and cache storage. Admission budgets combine execution and queue
// allowances, including jobs awaiting persistence. User and credential queue
// allowances default to half the global queue (at least one). Zero values select
// defaults. Storage limits are checked before admission and periodically during
// execution; a process can
// write additional bytes between checks. The cache must be a dedicated local
// Linux directory, and Repository must persist records in the catalog database.
type Options struct {
	Root                string
	FFmpegPath          string
	Threads             int
	MaxJobs             int
	MaxUserJobs         int
	MaxSessionJobs      int
	MaxQueueJobs        int
	MaxUserQueueJobs    int
	MaxSessionQueueJobs int
	MaxRetainedJobs     int
	MaxReaders          int
	MaxJobReaders       int
	MaxBytes            int64
	MaxJobBytes         int64
	MinFreeBytes        int64
	IdleTimeout         time.Duration
	StartupTimeout      time.Duration
	NoProgressTimeout   time.Duration
	MaxRuntime          time.Duration
	Repository          Repository
	// PlaybackAdmission and PlaybackStopped are memory-only lifetime hooks.
	// They run under Manager.mu and must not perform IO, database work or call
	// Manager methods. Admission returns one operation context and release; the
	// release also runs under mu after normal Ensure input ownership handling.
	// Neither hook changes the manager-owned lifetime context of an accepted job.
	PlaybackAdmission func(context.Context, Spec) (context.Context, func(), error)
	PlaybackStopped   func(Spec) bool
	// SourceRead prepares a catalog-issued route before the input enters the
	// queue. Preparation follows the first context; retained source ownership
	// follows the manager-owned job context, independently of the HTTP request.
	// The callback runs outside manager locks and must not classify Spec paths.
	SourceRead func(context.Context, context.Context, Spec, *os.File) (SourceReadLifetime, error)
	// ValidateHardware rechecks the captured device against the process's
	// startup-authorized inventory after queue waits and before execution. It
	// must be bounded and must not execute a hardware capability probe.
	ValidateHardware func(context.Context, Plan) error
	// SubtitleSource reauthorizes an external track immediately before burn-in.
	// It returns bounded ASS bytes bound to Spec.Plan.Subtitle.ExternalTag.
	SubtitleSource func(context.Context, Spec) ([]byte, error)
	// BitmapSubtitleSource calls exactly one synchronous bounded consumer and
	// must not retain that callback. The loader owns the borrowed descriptors,
	// joins the consumer even on cancellation, and revalidates both the media
	// source and every sidecar component after the consumer returns.
	BitmapSubtitleSource func(context.Context, Spec, func(context.Context, media.ExternalSubtitleTimelineInput) error) error
	// LivePublish synchronously copies one complete aligned bundle into the
	// authorized time-shift store. Borrowed files expire when the call returns.
	// LiveSubtitle consumes a bounded incremental subtitle stream; it must not
	// infer cue completeness from an unrelated AV publication callback.
	LivePublish  func(context.Context, Spec, string, LiveSegment) error
	LiveSubtitle func(context.Context, Spec, string, int, io.Reader) error
	LiveCaption  func(context.Context, Spec, string, LiveCaptionSegment) error
	run          runnerFunc
	pollInterval time.Duration
}

type managedJob struct {
	// filesMu serializes filesystem work with this job's accounting changes.
	// Acquire it before Manager.mu; readers pin the job before waiting for it.
	filesMu    sync.Mutex
	record     Record
	input      *os.File
	bitmap     *os.File
	sourceRead SourceReadLifetime
	ctx        context.Context
	cancel     context.CancelFunc
	created    chan struct{}
	launch     chan struct{}
	done       chan struct{}
	changed    chan struct{}
	durable    bool
	running    bool
	finished   bool
	reclaiming bool
	directory  bool
	ready      bool
	mediaReady bool
	readers    int
	// accountingUnknown stays charged until guarded directory removal succeeds.
	accountingUnknown    bool
	accountingPending    bool
	productionSealed     bool
	productionSealSafe   bool
	windowInputKnown     bool
	windowInput          [MaxHLSRenditions]GeneratedInputEvidence
	stopCode             string
	started              time.Time
	lastProgress         time.Time
	outputTicks          int64
	progressSize         int64
	hlsClocks            [MaxHLSRenditions]HLSMuxClock
	hlsClockKnown        [MaxHLSRenditions]bool
	completion           *completionTicket
	finalizationQueued   bool
	subjectOwnershipHeld bool
}

// Manager owns every input accepted by Ensure and every process it starts.
// Authorization belongs to the caller: each lookup additionally requires an
// exact scope match, and no original authentication token is retained here.
type Manager struct {
	options Options
	cache   *cacheRoot
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	// filesMu is the filesystem lifetime gate. Individual job operations share
	// it, and shutdown takes it exclusively before closing the cache root.
	filesMu               sync.RWMutex
	jobs                  map[string]*managedJob
	bySpec                map[Spec]*managedJob
	queue                 []*managedJob
	running               int
	diagnosticReservation int
	runningUsers          map[string]int
	runningAuth           map[string]int
	bytes                 int64
	unaccountedJobs       int
	pendingAccountingJobs int
	maintenanceCursor     string
	readers               int
	cacheFailed           bool
	closing               bool
	closeErr              error
	wake                  chan struct{}
	loopDone              chan struct{}
	closed                chan struct{}
	completions           *completionTicketPool
	finalizers            *finalizationExecutor
	// runners covers accepted runner handoff, not terminal finalization.
	runners sync.WaitGroup
	// workers continues to cover explicitly reserved diagnostic ownership.
	workers sync.WaitGroup
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
	m.completions, err = newCompletionTicketPool(options.MaxRetainedJobs, m.signal)
	if err != nil {
		cancel()
		_ = cache.Close()
		return nil, err
	}
	m.finalizers, err = newFinalizationExecutor(m.completions, min(2, options.MaxJobs))
	if err != nil {
		m.completions.close()
		cancel()
		_ = cache.Close()
		return nil, err
	}
	if err = startupCtx.Err(); err != nil {
		// No accepted runner or ticket can reach these workers yet. Stop and
		// actually join this fixed set before closing its rooted filesystem.
		m.finalizers.stopAfterDrain()
		joinErr := m.finalizers.wait(context.Background())
		cancel()
		return nil, errors.Join(err, joinErr, cache.Close())
	}
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
	if o.MaxUserQueueJobs == 0 {
		o.MaxUserQueueJobs = max(1, o.MaxQueueJobs/2)
	}
	if o.MaxSessionQueueJobs == 0 {
		o.MaxSessionQueueJobs = max(1, o.MaxQueueJobs/2)
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
		o.MaxUserQueueJobs < 1 || o.MaxUserQueueJobs > o.MaxQueueJobs ||
		o.MaxSessionQueueJobs < 1 || o.MaxSessionQueueJobs > o.MaxQueueJobs ||
		o.MaxReaders < 1 || o.MaxReaders > 4096 || o.MaxJobReaders < 1 || o.MaxJobReaders > o.MaxReaders ||
		o.MaxBytes < 1 || o.MaxJobBytes < 1 || o.MaxJobBytes > o.MaxBytes || o.MinFreeBytes < 0 ||
		o.IdleTimeout < time.Millisecond || o.StartupTimeout < time.Millisecond || o.NoProgressTimeout < time.Millisecond ||
		o.MaxRuntime < time.Millisecond || o.pollInterval < time.Millisecond || o.pollInterval > time.Second {
		return o, ErrInvalidOptions
	}
	return o, nil
}

func validScope(scope Scope) bool {
	if scope.ApplicationKey && scope.UserID != "" || !scope.ApplicationKey && !validManagerIdentifier(scope.UserID, 256, false) {
		return false
	}
	if scope.ApplicationKey && !validManagerIdentifier(scope.ApplicationClientID, 256, false) ||
		!scope.ApplicationKey && scope.ApplicationClientID != "" {
		return false
	}
	for _, value := range []string{scope.AuthSessionID, scope.PlaySessionID, scope.ItemID, scope.SourceID} {
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
	return m.ensureInputs(ctx, spec, StreamInputs{Media: input})
}

func (m *Manager) ensureInputs(ctx context.Context, spec Spec, inputs StreamInputs) (result Record, resultErr error) {
	input := inputs.Media
	owned := false
	returned, locked := false, false
	var admissionRelease func()
	var sourceRead SourceReadLifetime
	var sourceCancel context.CancelFunc
	lock := func() { m.mu.Lock(); locked = true }
	unlock := func() { locked = false; m.mu.Unlock() }
	defer func() {
		// Context and memory callbacks can exit abnormally. Their original
		// operation reference remains held, while no admission mutex is stranded.
		if locked {
			unlock()
		}
		if !owned {
			resultErr = errors.Join(resultErr, closeSourceReadInputs(inputs, sourceRead))
			if sourceCancel != nil {
				sourceCancel()
			}
		}
		if returned && admissionRelease != nil {
			m.releasePlaybackAdmission(admissionRelease)
		}
	}()
	result, resultErr = func() (Record, error) {
		admissionChecked := false
		if ctx == nil {
			return Record{}, ErrInvalidOptions
		}
		if err := ctx.Err(); err != nil {
			return Record{}, err
		}
		if !validScope(spec.Scope) || !validManagerIdentifier(spec.SourceStamp, 256, false) {
			return Record{}, ErrInvalidScope
		}
		if spec.Plan.ExecutionVersion == 0 && spec.Plan.Execution == (ExecutionOptions{}) {
			var err error
			spec.Plan, err = CaptureExecution(spec.Plan, DefaultExecutionOptions(m.options.Threads))
			if err != nil {
				return Record{}, err
			}
		}
		if err := ValidatePlan(spec.Plan); err != nil {
			return Record{}, err
		}
		if spec.Plan.SourceMode == "stream" && m.options.LivePublish == nil {
			return Record{}, ErrInvalidOptions
		}
		if err := validateStreamInputs(inputs, spec.Plan); err != nil {
			return Record{}, err
		}
		if spec.Plan.Subtitle.Mode == "burn" && spec.Plan.Subtitle.ExternalTag != "" {
			if IsBitmapSubtitle(spec.Plan.Subtitle.Codec) && m.options.BitmapSubtitleSource == nil ||
				!IsBitmapSubtitle(spec.Plan.Subtitle.Codec) && m.options.SubtitleSource == nil {
				return Record{}, ErrInvalidOptions
			}
		}
		if input == nil {
			return Record{}, ErrInvalidInput
		}
		_, err := validateSourceInput(input, spec.Plan)
		if err != nil {
			return Record{}, ErrInvalidInput
		}
		jobCtx, cancel := context.WithCancel(m.ctx)
		sourceCancel = cancel
		if spec.Plan.SourceMode != "stream" && m.options.SourceRead != nil {
			sourceRead, err = m.options.SourceRead(ctx, jobCtx, spec, input)
			if err != nil {
				return Record{}, err
			}
			if sourceRead == nil {
				return Record{}, ErrInvalidScope
			}
		}
	retryAdmission:
		if err := ctx.Err(); err != nil {
			return Record{}, err
		}
		lock()
		if m.closing {
			unlock()
			return Record{}, ErrManagerClosed
		}
		if m.cacheFailed {
			unlock()
			return Record{}, ErrOutputUnavailable
		}
		if !admissionChecked {
			admitted, release, admissionErr := m.admitPlaybackLocked(ctx, spec)
			admissionRelease, admissionChecked = release, true
			if admissionErr != nil {
				unlock()
				return Record{}, admissionErr
			}
			ctx = admitted
			if err := ctx.Err(); err != nil {
				unlock()
				return Record{}, err
			}
		}
		if m.playbackStoppedLocked(spec) {
			unlock()
			return Record{}, ErrJobCancelled
		}
		if existing := m.bySpec[spec]; existing != nil && existing.stopCode == "" {
			m.touchLocked(existing)
			record, created := existing.record, existing.created
			unlock()
			select {
			case <-ctx.Done():
				return Record{}, ctx.Err()
			case <-created:
			}
			lock()
			record = existing.record
			switch {
			case m.closing:
				err = ErrManagerClosed
			case m.cacheFailed:
				err = ErrOutputUnavailable
			case m.playbackStoppedLocked(spec):
				err = ErrJobCancelled
			case m.jobs[record.ID] != existing || existing.reclaiming:
				unlock()
				goto retryAdmission
			default:
				err = jobError(existing)
			}
			unlock()
			return record, err
		}
		if m.unaccountedJobs != 0 || m.pendingAccountingJobs != 0 {
			unlock()
			return Record{}, ErrOutputUnavailable
		}
		if !m.admissionAvailableLocked(spec.Scope) {
			workAvailable := m.workAdmissionAvailableLocked(spec.Scope)
			unlock()
			if !workAvailable {
				return Record{}, ErrBusy
			}
			reclaimed, err := m.reclaimForAdmission(ctx, spec)
			if err != nil {
				return Record{}, err
			}
			if reclaimed {
				goto retryAdmission
			}
			return Record{}, ErrBusy
		}
		if m.bytes >= m.options.MaxBytes {
			unlock()
			return Record{}, ErrQuota
		}
		unlock()
		m.filesMu.RLock()
		free, err := m.cache.FreeBytes()
		m.filesMu.RUnlock()
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
		j := &managedJob{record: Record{ID: hex.EncodeToString(randomID[:]), Spec: spec, State: "queued", CreatedAt: now, UpdatedAt: now, LastAccessAt: now},
			input: input, bitmap: inputs.Bitmap, sourceRead: sourceRead, ctx: jobCtx, cancel: cancel, created: make(chan struct{}), launch: make(chan struct{}), done: make(chan struct{}), changed: make(chan struct{})}
		lock()
		if m.closing {
			unlock()
			return Record{}, ErrManagerClosed
		}
		if m.cacheFailed {
			unlock()
			return Record{}, ErrOutputUnavailable
		}
		if m.playbackStoppedLocked(spec) {
			unlock()
			return Record{}, ErrJobCancelled
		}
		// Admission is repeated after the filesystem call: another request may have
		// inserted the same spec or filled the queue while that call was in flight.
		if existing := m.bySpec[spec]; existing != nil && existing.stopCode == "" {
			unlock()
			goto retryAdmission
		}
		if m.unaccountedJobs != 0 || m.pendingAccountingJobs != 0 {
			unlock()
			return Record{}, ErrOutputUnavailable
		}
		if !m.admissionAvailableLocked(spec.Scope) {
			workAvailable := m.workAdmissionAvailableLocked(spec.Scope)
			unlock()
			if !workAvailable {
				return Record{}, ErrBusy
			}
			reclaimed, err := m.reclaimForAdmission(ctx, spec)
			if err != nil {
				return Record{}, err
			}
			if reclaimed {
				goto retryAdmission
			}
			return Record{}, ErrBusy
		}
		if m.bytes >= m.options.MaxBytes {
			unlock()
			return Record{}, ErrQuota
		}
		completion, err := m.completions.reserve()
		if err != nil {
			unlock()
			return Record{}, err
		}
		// This fresh ticket belongs to j before any worker can observe it. Every
		// accepted creating/queued job retains it through terminal handling.
		j.completion = completion
		m.jobs[j.record.ID], m.bySpec[spec] = j, j
		m.queue = append(m.queue, j)
		initialRecord := j.record
		m.runners.Add(1)
		owned = true
		unlock()
		go m.runJob(j)
		createCtx, cancelCreate := context.WithTimeout(ctx, 5*time.Second)
		stopCreate := context.AfterFunc(m.ctx, cancelCreate)
		err = m.options.Repository.Create(createCtx, initialRecord)
		stopCreate()
		cancelCreate()
		lock()
		stopped := m.playbackStoppedLocked(spec)
		if err != nil {
			j.stopCode = "persistence"
			j.cancel()
		} else {
			j.durable = true
			if ctx.Err() != nil || stopped {
				m.stopLocked(j, "cancelled")
			}
		}
		close(j.created)
		m.notifyLocked(j)
		record := j.record
		closing := m.closing
		unlock()
		m.signal()
		if err != nil {
			return record, ErrPersistence
		}
		if err = ctx.Err(); err != nil {
			return record, err
		}
		if stopped {
			return record, ErrJobCancelled
		}
		if closing {
			return record, ErrManagerClosed
		}
		return record, nil
	}()
	returned = true
	return result, resultErr
}

func (m *Manager) admitPlaybackLocked(ctx context.Context, spec Spec) (context.Context, func(), error) {
	if m.options.PlaybackAdmission == nil {
		return ctx, nil, nil
	}
	returned := false
	defer func() {
		if !returned {
			m.cacheFailed = true
		}
	}()
	admitted, release, err := m.options.PlaybackAdmission(ctx, spec)
	returned = true
	if err == nil && (admitted == nil || release == nil) {
		err = ErrInvalidOptions
	}
	return admitted, release, err
}

func (m *Manager) playbackStoppedLocked(spec Spec) bool {
	if m.options.PlaybackStopped == nil {
		return false
	}
	returned := false
	defer func() {
		if !returned {
			m.cacheFailed = true
		}
	}()
	stopped := m.options.PlaybackStopped(spec)
	returned = true
	return stopped
}

func (m *Manager) releasePlaybackAdmission(release func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	returned := false
	defer func() {
		if !returned {
			m.cacheFailed = true
		}
	}()
	release()
	returned = true
}

// Admission reserves each subject's execution allowance as well as its waiting
// allowance. Counting every accepted, unfinished job includes creation and
// scheduling races without making idle execution slots unavailable merely
// because another subject has filled its queue. Application clients sharing a
// credential share the credential allowance; user sessions also share a user
// allowance. Stopping jobs remain bounded by MaxRetainedJobs until reaped.
func (m *Manager) admissionAvailableLocked(scope Scope) bool {
	return len(m.jobs) < m.options.MaxRetainedJobs && m.workAdmissionAvailableLocked(scope)
}

func (m *Manager) workAdmissionAvailableLocked(scope Scope) bool {
	active, user, auth := 0, 0, 0
	for _, j := range m.jobs {
		if j.finished || j.stopCode != "" {
			continue
		}
		active++
		owner := j.record.Spec.Scope
		if !scope.ApplicationKey && !owner.ApplicationKey && owner.UserID == scope.UserID {
			user++
		}
		if owner.AuthSessionID == scope.AuthSessionID {
			auth++
		}
	}
	// A small retained-record budget must not erase the execution capacity
	// reserved for other subjects. Completed history is still independently
	// bounded by the global retention limit and reclaimed under admission pressure.
	userLimit := min(m.options.MaxUserJobs+m.options.MaxUserQueueJobs,
		m.options.MaxRetainedJobs-m.options.MaxJobs+m.options.MaxUserJobs)
	authLimit := min(m.options.MaxSessionJobs+m.options.MaxSessionQueueJobs,
		m.options.MaxRetainedJobs-m.options.MaxJobs+m.options.MaxSessionJobs)
	return active < m.options.MaxJobs+m.options.MaxQueueJobs &&
		(scope.ApplicationKey || user < userLimit) && auth < authLimit
}

// reclaimForAdmission retires one unpinned terminal record only when retention
// is the remaining admission constraint. Finished includes process reaping,
// input closure, and the final persistence attempt. Invalidated history is
// preferred over reusable output, then the least recently accessed job wins.
func (m *Manager) reclaimForAdmission(ctx context.Context, spec Spec) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	m.filesMu.RLock()
	defer m.filesMu.RUnlock()
	m.mu.Lock()
	if err := ctx.Err(); err != nil {
		m.mu.Unlock()
		return false, err
	}
	if m.closing {
		m.mu.Unlock()
		return false, ErrManagerClosed
	}
	if m.cacheFailed {
		m.mu.Unlock()
		return false, ErrOutputUnavailable
	}
	if existing := m.bySpec[spec]; existing != nil && existing.stopCode == "" {
		m.mu.Unlock()
		return true, nil
	}
	if m.unaccountedJobs != 0 || m.pendingAccountingJobs != 0 {
		m.mu.Unlock()
		return false, ErrOutputUnavailable
	}
	if !m.workAdmissionAvailableLocked(spec.Scope) {
		m.mu.Unlock()
		return false, nil
	}
	if len(m.jobs) < m.options.MaxRetainedJobs {
		m.mu.Unlock()
		return true, nil
	}
	var victim *managedJob
	for _, j := range m.jobs {
		if !j.finished || j.readers != 0 || j.reclaiming || !completionReturnedForReclaim(j) {
			continue
		}
		if victim == nil || j.stopCode != "" && victim.stopCode == "" ||
			(j.stopCode != "") == (victim.stopCode != "") &&
				(j.record.LastAccessAt.Before(victim.record.LastAccessAt) ||
					j.record.LastAccessAt.Equal(victim.record.LastAccessAt) && j.record.ID < victim.record.ID) {
			victim = j
		}
	}
	if victim == nil {
		m.mu.Unlock()
		return false, nil
	}
	m.mu.Unlock()
	// A scan may still own this job's filesystem state. Waiting without mu
	// lets other jobs open output, finish and release their execution slots.
	victim.filesMu.Lock()
	defer victim.filesMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if m.closing {
		return false, ErrManagerClosed
	}
	if m.cacheFailed {
		return false, ErrOutputUnavailable
	}
	if existing := m.bySpec[spec]; existing != nil && existing.stopCode == "" {
		return true, nil
	}
	if m.unaccountedJobs != 0 || m.pendingAccountingJobs != 0 {
		return false, ErrOutputUnavailable
	}
	if !m.workAdmissionAvailableLocked(spec.Scope) {
		return false, nil
	}
	if len(m.jobs) < m.options.MaxRetainedJobs || m.jobs[victim.record.ID] != victim {
		return true, nil
	}
	reclaimed := m.reclaimLocked(victim, true)
	if m.cacheFailed {
		return false, ErrOutputUnavailable
	}
	return reclaimed, nil
}

// Health reports whether the engine can accept work, independently of temporary
// queue, reader, or storage pressure. A guarded cache cleanup failure is sticky
// and requires operator repair and a service restart. Codes contain no paths,
// credentials, or raw filesystem errors.
type Health struct {
	Available bool
	Code      string
}

func (m *Manager) Health() Health {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cacheFailed {
		return Health{Code: "cache_unavailable"}
	}
	if m.closing {
		return Health{Code: "manager_closed"}
	}
	return Health{Available: true, Code: "ready"}
}

// ReserveDiagnostic occupies one global execution slot on this live manager.
// At most one diagnostic can hold a reservation; no encoding or playback
// record is created. The caller must close every diagnostic process before
// releasing the slot, including after cancellation or a failed stage. A failed
// diagnostic close must retain the reservation so Manager.Close cannot report
// resource closure prematurely. Repeated calls to the returned release are safe.
func (m *Manager) ReserveDiagnostic() (func(), error) {
	if m == nil {
		return nil, ErrManagerClosed
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closing {
		return nil, ErrManagerClosed
	}
	if m.cacheFailed {
		return nil, ErrOutputUnavailable
	}
	if m.diagnosticReservation != 0 || m.running >= m.options.MaxJobs {
		return nil, ErrBusy
	}
	m.diagnosticReservation = 1
	// Positive additions are serialized with Close, just like admitted job
	// workers. Shutdown waits without holding mu until the owner releases.
	m.workers.Add(1)
	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			m.diagnosticReservation = 0
			m.mu.Unlock()
			m.workers.Done()
			m.signal()
		})
	}, nil
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
	if readableSealedProduction(j) {
		return nil
	}
	if j.stopCode != "" || j.record.State == "failed" || j.record.State == "cancelled" || j.record.State == "interrupted" {
		code := j.stopCode
		if code == "" {
			code = j.record.ErrorCode
		}
		switch code {
		case "cancelled", "session_cancelled", "source_replaced", "idle_timeout", "manager_closed":
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
	if j == nil || j.reclaiming || j.record.Spec.Scope != scope {
		return nil, ErrJobNotFound
	}
	return j, nil
}

// Snapshot returns the current in-memory status without refreshing its idle
// lease. A stopped or failed job returns both its record and its classified
// error. A completed job can therefore retain State "completed" while returning
// ErrJobCancelled after its cached output has been explicitly invalidated.
// A retained partial window reports cancelled/production_sealed without an error;
// its missing artifacts cannot wait for more production.
func (m *Manager) Snapshot(scope Scope, id string) (Record, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, err := m.lookupLocked(scope, id)
	if err != nil {
		return Record{}, err
	}
	record := j.record
	record.ProductionSealed = j.productionSealed
	return record, jobError(j)
}

// WaitReady requires each planned HLS rendition to have a published playlist,
// a completed segment, and its initialization file when using fragmented MP4.
// Progressive output instead requires a nonempty stream whose media payload the
// runner has confirmed. It does not expose temporary or header-only output.
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
		noFutureOutput := j.productionSealed || j.finished
		m.mu.Unlock()
		if err != nil {
			return record, err
		}
		if ready {
			return record, nil
		}
		if noFutureOutput {
			return record, ErrOutputUnavailable
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
	jobID   string
	once    sync.Once
	release func()
	err     error
}

// EncodingID ties private timing evidence to the exact pinned media object.
func (h *ReadHandle) EncodingID() string { return h.jobID }

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
// artifact. It returns ErrOutputUnavailable until the job is ready and the final
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
	kind, valid := HLSArtifact(name)
	if !valid {
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
	if j.record.Spec.Plan.OutputMode != "" || j.record.Spec.Plan.SourceMode == "stream" {
		m.mu.Unlock()
		return attempt, ErrOutputUnavailable
	}
	m.touchLocked(j)
	attempt.changed = j.changed
	if !j.ready {
		attempt.pending = !j.finished && !j.productionSealed
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
	m.filesMu.RLock()
	j.filesMu.Lock()
	file, openErr := m.cache.OpenJobFile(id, name)
	if errors.Is(openErr, ErrCacheUnsafe) {
		m.mu.Lock()
		m.rejectCacheObservationLocked(j)
		m.mu.Unlock()
	}
	j.filesMu.Unlock()
	m.filesMu.RUnlock()
	if openErr == nil {
		info, statErr := file.Stat()
		if statErr != nil || info.Size() <= 0 || info.Size() > m.options.MaxJobBytes || (kind == "playlist" && info.Size() > MaxPlaylistBytes) {
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
			attempt.pending = !j.finished && !j.productionSealed
			err = ErrOutputUnavailable
		} else if openErr != nil {
			attempt.pending = errors.Is(openErr, os.ErrNotExist) && !j.finished && !j.productionSealed
			err = ErrOutputUnavailable
		}
	}
	m.mu.Unlock()
	if errors.Is(openErr, ErrCacheUnsafe) && errors.Is(err, ErrJobFailed) {
		err = ErrOutputUnavailable
	}
	if err != nil {
		if file != nil {
			_ = file.Close()
		}
		m.releaseReader(j)
		return attempt, err
	}
	attempt.handle = &ReadHandle{File: file, jobID: id, release: func() { m.releaseReader(j) }}
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

// CancelPlayback immediately invalidates all jobs belonging to one playback
// session, including producers no longer tracked by an HTTP registration.
// The caller must authorize both identifiers. Processes are reaped and files
// are reclaimed in the background after their outstanding readers close.
func (m *Manager) CancelPlayback(authID, playID string) {
	if authID == "" || playID == "" {
		return
	}
	m.mu.Lock()
	for _, j := range m.jobs {
		scope := j.record.Spec.Scope
		if scope.AuthSessionID != authID || scope.PlaySessionID != playID {
			continue
		}
		if j.finished {
			m.invalidateFinishedLocked(j, "cancelled")
		} else {
			m.stopLocked(j, "cancelled")
		}
	}
	m.mu.Unlock()
	m.signal()
}

// CancelFileHLSPlayback invalidates only file-HLS jobs for an authorized play.
// It is the restrictive sweep for a correlated early Stop intent; progressive
// and stream jobs keep their existing post-commit cancellation ordering.
// Creating and unattached jobs use the same mutex as normal admission.
func (m *Manager) CancelFileHLSPlayback(authID, playID string) {
	if authID == "" || playID == "" {
		return
	}
	m.mu.Lock()
	for _, j := range m.jobs {
		scope, plan := j.record.Spec.Scope, j.record.Spec.Plan
		if scope.AuthSessionID != authID || scope.PlaySessionID != playID ||
			plan.SourceMode == "stream" || plan.OutputMode != "" && plan.OutputMode != "hls" {
			continue
		}
		if j.finished {
			m.invalidateFinishedLocked(j, "cancelled")
		} else {
			m.stopLocked(j, "cancelled")
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
		periodic := false
		select {
		case <-m.ctx.Done():
			return
		case <-m.wake:
		case <-ticker.C:
			periodic = true
		}
		// Reader releases and queue changes must not turn every HTTP artifact
		// request into a full scan of every retained output directory.
		m.schedule()
		m.maintainJobs(periodic)
		m.schedule()
	}
}

func (m *Manager) schedule() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closing || m.cacheFailed || m.unaccountedJobs != 0 || m.pendingAccountingJobs != 0 {
		return
	}
	remaining := m.queue[:0]
	for _, j := range m.queue {
		if m.cacheFailed {
			remaining = append(remaining, j)
			continue
		}
		if j.finished || j.stopCode != "" {
			continue
		}
		if m.playbackStoppedLocked(j.record.Spec) {
			m.stopLocked(j, "cancelled")
			continue
		}
		if j.completion == nil || j.completion.pool != m.completions || j.finalizationQueued {
			// A production admission never manufactures a missing reservation at
			// launch. Preserve this record and fence the inconsistent owner.
			m.cacheFailed = true
			m.stopLocked(j, "cache_unavailable")
			remaining = append(remaining, j)
			continue
		}
		if !j.durable || m.running+m.diagnosticReservation >= m.options.MaxJobs ||
			(!j.record.Spec.Scope.ApplicationKey && m.runningUsers[j.record.Spec.Scope.UserID] >= m.options.MaxUserJobs) ||
			m.runningAuth[j.record.Spec.Scope.AuthSessionID] >= m.options.MaxSessionJobs || m.bytes >= m.options.MaxBytes {
			remaining = append(remaining, j)
			continue
		}
		j.running = true
		j.subjectOwnershipHeld = true
		j.started = time.Now().UTC()
		j.lastProgress = j.started
		j.record.State, j.record.UpdatedAt = "running", j.started
		m.running++
		if !j.record.Spec.Scope.ApplicationKey {
			m.runningUsers[j.record.Spec.Scope.UserID]++
		}
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
	defer m.runners.Done()
	handedOff := false
	defer func() {
		if !handedOff {
			_ = recover()
			// A panic or Goexit is not normal runner drain. Retain the original
			// execution, subject and completion ownership for explicit recovery.
			m.quarantineJobFinalization(j, errJobResourceOwnership)
		}
	}()
	handoff := func(runErr error, failure *ProgressFailure) {
		if m.enqueueFinalization(j, runErr, failure) == nil {
			handedOff = true
		}
	}
	<-j.created
	select {
	case <-j.ctx.Done():
		handoff(j.ctx.Err(), nil)
		return
	case <-j.launch:
	}
	if err := m.persist(j); err != nil {
		m.fail(j, "persistence")
		handoff(err, nil)
		return
	}
	if err := j.ctx.Err(); err != nil {
		handoff(err, nil)
		return
	}
	m.filesMu.RLock()
	j.filesMu.Lock()
	free, err := m.cache.FreeBytes()
	if err == nil && free < m.options.MinFreeBytes {
		err = ErrQuota
	}
	if err == nil {
		err = m.cache.CreateJob(j.record.ID)
	}
	j.filesMu.Unlock()
	m.filesMu.RUnlock()
	if err != nil {
		if errors.Is(err, errCacheCreationUnaccounted) {
			m.mu.Lock()
			j.directory = true
			m.markAccountingUnknownLocked(j)
			m.mu.Unlock()
		}
		if errors.Is(err, ErrQuota) {
			m.fail(j, "cache_space")
		} else {
			m.fail(j, "cache_unavailable")
		}
		handoff(err, nil)
		return
	}
	m.mu.Lock()
	j.directory = true
	m.mu.Unlock()
	m.filesMu.RLock()
	directory, err := m.cache.JobPath(j.record.ID)
	m.filesMu.RUnlock()
	if err != nil {
		m.fail(j, "cache_unavailable")
		handoff(err, nil)
		return
	}
	if m.options.ValidateHardware != nil {
		checkContext, cancelCheck := context.WithTimeout(j.ctx, 5*time.Second)
		err := m.options.ValidateHardware(checkContext, j.record.Spec.Plan)
		cancelCheck()
		if err != nil {
			m.fail(j, "hardware_unavailable")
			handoff(err, nil)
			return
		}
	}
	runContext := withSubtitleSource(j.ctx, j.record.Spec, m.options.SubtitleSource)
	runContext = withBitmapSubtitleSource(runContext, j.record.Spec, m.options.BitmapSubtitleSource)
	if j.sourceRead != nil {
		runContext = j.sourceRead.Context(runContext)
	}
	if j.record.Spec.Plan.SourceMode == "stream" {
		runContext = withLiveRuntime(runContext, liveRuntime{spec: j.record.Spec, jobID: j.record.ID, inputs: StreamInputs{Media: j.input, Bitmap: j.bitmap},
			maxBytes: min(MaxLiveScratchBytes, m.options.MaxJobBytes), timeout: m.options.NoProgressTimeout, publish: m.options.LivePublish, subtitle: m.options.LiveSubtitle, caption: m.options.LiveCaption})
	}
	result, err := m.options.run(runContext, m.options.FFmpegPath, directory, j.input, j.record.Spec.Plan, j.record.Spec.Plan.Execution.Threads, func(p Progress) {
		m.mu.Lock()
		if clock := p.HLSClock; clock != nil && needsHLSClock(j.record.Spec.Plan) && clock.Rendition >= 0 && clock.Rendition < max(1, j.record.Spec.Plan.HLS.RenditionCount) {
			if _, err := clock.ticks(j.record.Spec.Plan.SourceMode != "stream"); err == nil && !j.hlsClockKnown[clock.Rendition] {
				j.hlsClocks[clock.Rendition], j.hlsClockKnown[clock.Rendition] = *clock, true
				m.notifyLocked(j)
			}
		}
		becameReady := p.Ready && !j.mediaReady && (j.record.Spec.Plan.OutputMode == "progressive" || j.record.Spec.Plan.SourceMode == "stream")
		if becameReady {
			j.mediaReady = true
			m.notifyLocked(j)
		}
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
		if becameReady {
			m.signal()
		}
	})
	m.mu.Lock()
	j.productionSealSafe = result.ProductionSealSafe
	if result.WindowInputEvidence != nil {
		j.windowInput, j.windowInputKnown = *result.WindowInputEvidence, true
	}
	m.mu.Unlock()
	handoff(err, result.ProgressFailure)
}

func logManagerProgressFailure(jobID, errorClass string, failure *ProgressFailure) {
	attrs := []slog.Attr{
		slog.String("job_id", jobID), slog.String("error_class", errorClass),
		slog.String("phase", failure.Phase), slog.String("reason", failure.Reason), slog.String("field", failure.Field),
		slog.Int("line_bytes", failure.LineBytes), slog.Int("captured_bytes", failure.CapturedBytes),
		slog.Bool("truncated", failure.Truncated), slog.String("line_sha256", failure.LineSHA256),
		slog.Bool("previous_known", failure.Previous != nil), slog.Bool("wait_delay", failure.WaitDelay),
		slog.String("wait_error_class", failure.WaitErrorClass), slog.Int("exit_code", failure.ExitCode),
	}
	if failure.SafeValue != "" {
		attrs = append(attrs, slog.String("safe_value", failure.SafeValue))
	}
	if previous := failure.Previous; previous != nil {
		attrs = append(attrs, slog.Int64("output_ticks", previous.OutputTicks), slog.Int64("bytes", previous.Bytes),
			slog.Bool("ended", previous.Ended))
	}
	// The fixed terminal worker retains its completion and subject ownership
	// while synchronous diagnostic output can block. Inspection already returned
	// global execution capacity; normal terminal return completes the lifetime.
	slog.LogAttrs(context.Background(), slog.LevelWarn, "transcode progress rejected", attrs...)
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

// Unticketed completed records exist only in recovery/test fixtures. Every
// production Ensure owns an exact ticket; callback completion must return it
// before guarded record removal can make its metadata capacity reusable.
func completionReturnedForReclaim(j *managedJob) bool {
	if j.completion == nil {
		return !j.finalizationQueued
	}
	j.completion.pool.mu.Lock()
	defer j.completion.pool.mu.Unlock()
	return j.completion.phase == completionReleased
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

func (m *Manager) maintain() { m.maintainJobs(true) }

func (m *Manager) maintainJobs(periodic bool) {
	m.mu.Lock()
	var jobs []*managedJob
	for _, j := range m.jobs {
		if !j.reclaiming && j.directory &&
			(j.running || j.record.State == "completed" || readableSealedProduction(j)) &&
			(periodic || j.mediaReady && !j.ready) {
			jobs = append(jobs, j)
		}
	}
	cursor := m.maintenanceCursor
	m.mu.Unlock()
	// Stable ordering and a persisted cursor give both active producers and
	// completed caches a fair share of the finite inspection allowance. Warm
	// reader wakeups do not copy or sort unrelated retained history.
	slices.SortFunc(jobs, func(a, b *managedJob) int { return strings.Compare(a.record.ID, b.record.ID) })
	start := 0
	for start < len(jobs) && jobs[start].record.ID <= cursor {
		start++
	}
	if start == len(jobs) {
		start = 0
	}
	budget := cacheMaintenanceGlobalBudget
	for offset := range len(jobs) {
		if budget < 6 {
			break
		}
		j := jobs[(start+offset)%len(jobs)]
		m.mu.Lock()
		scan := m.jobs[j.record.ID] == j && !j.reclaiming && j.directory &&
			(j.running || j.record.State == "completed" || readableSealedProduction(j)) &&
			(periodic || j.mediaReady && !j.ready)
		m.mu.Unlock()
		if !scan {
			continue
		}
		m.filesMu.RLock()
		j.filesMu.Lock()
		m.mu.Lock()
		scan = m.jobs[j.record.ID] == j && !j.reclaiming && j.directory &&
			(j.running || j.record.State == "completed" || readableSealedProduction(j))
		m.mu.Unlock()
		if !scan {
			j.filesMu.Unlock()
			m.filesMu.RUnlock()
			continue
		}
		inspection, err := m.cache.MaintainPlanJob(j.record.ID, j.record.Spec.Plan, min(budget, cacheMaintenanceJobBudget))
		size, ready := inspection.bytes, inspection.ready
		budget -= inspection.work
		m.mu.Lock()
		m.maintenanceCursor = j.record.ID
		if err != nil {
			m.setAccountingPendingLocked(j, false)
			m.markAccountingUnknownLocked(j)
			if !j.finished {
				m.stopLocked(j, "cache_unavailable")
			} else {
				m.invalidateFinishedLocked(j, "cache_unavailable")
			}
		} else {
			m.setAccountingPendingLocked(j, inspection.pending)
			if inspection.pending {
				// Rename/source events may span batches. A partial census can
				// increase a known charge but never release storage or publish
				// initial readiness before its change accounting closes.
				size = max(size, j.record.OutputBytes)
				ready = j.ready
			}
			if j.record.Spec.Plan.SourceMode == "stream" {
				ready = j.mediaReady
			}
			if j.record.Spec.Plan.OutputMode == "progressive" {
				ready = ready && j.mediaReady
				if size < j.record.OutputBytes {
					if j.finished {
						m.invalidateFinishedLocked(j, "invalid_output")
					} else {
						m.stopLocked(j, "invalid_output")
					}
				}
			}
			if j.accountingUnknown {
				size = max(size, j.record.OutputBytes)
			}
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
		j.filesMu.Unlock()
		m.filesMu.RUnlock()
	}
	var free int64
	var freeErr error
	if periodic {
		m.filesMu.RLock()
		free, freeErr = m.cache.FreeBytes()
		m.filesMu.RUnlock()
	}
	now := time.Now().UTC()
	m.mu.Lock()
	var reclaim []*managedJob
	for _, j := range m.jobs {
		if j.finished {
			if j.readers == 0 && !j.reclaiming &&
				(now.Sub(j.record.LastAccessAt) >= m.options.IdleTimeout || j.directory && j.stopCode != "") {
				reclaim = append(reclaim, j)
			}
			continue
		}
		switch {
		case m.cacheFailed:
			m.stopLocked(j, "cache_unavailable")
		case freeErr != nil:
			m.stopLocked(j, "cache_unavailable")
		case periodic && free < m.options.MinFreeBytes:
			m.stopLocked(j, "cache_space")
		case m.bytes > m.options.MaxBytes:
			m.stopLocked(j, "cache_quota")
		case j.readers == 0 && now.Sub(j.record.LastAccessAt) > m.options.IdleTimeout:
			m.stopLocked(j, "idle_timeout")
		case periodic && j.running && j.record.Spec.Plan.SourceMode != "stream" && now.Sub(j.started) > m.options.MaxRuntime:
			m.stopLocked(j, "runtime_timeout")
		case periodic && j.running && !j.ready && now.Sub(j.started) > m.options.StartupTimeout:
			m.stopLocked(j, "startup_timeout")
		case periodic && j.running && now.Sub(j.lastProgress) > m.options.NoProgressTimeout:
			m.stopLocked(j, "progress_timeout")
		}
	}
	m.mu.Unlock()
	for _, j := range reclaim {
		m.reclaim(j, false)
	}
}

func (m *Manager) reclaim(j *managedJob, force bool) bool {
	m.filesMu.RLock()
	defer m.filesMu.RUnlock()
	j.filesMu.Lock()
	defer j.filesMu.Unlock()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reclaimLocked(j, force)
}

// reclaimLocked requires the shared filesystem gate, j.filesMu and mu, and
// returns holding all three. Keeping the record reserved until guarded cleanup
// finishes prevents admission from using its slot before a cleanup failure
// makes the cache unavailable. Lookups cannot pin new readers while mu is
// released for filesystem work.
func (m *Manager) reclaimLocked(j *managedJob, force bool) bool {
	if m.jobs[j.record.ID] != j || !j.finished || j.readers != 0 || j.reclaiming || !completionReturnedForReclaim(j) {
		return false
	}
	expired := force || time.Since(j.record.LastAccessAt) >= m.options.IdleTimeout
	removeFiles := j.directory && (expired || j.stopCode != "")
	if removeFiles {
		j.directory = false
	}
	if expired {
		j.reclaiming = true
		if m.bySpec[j.record.Spec] == j {
			delete(m.bySpec, j.record.Spec)
		}
		m.notifyLocked(j)
	}
	if removeFiles {
		m.mu.Unlock()
		err := m.cache.RemoveJob(j.record.ID)
		m.mu.Lock()
		if err == nil {
			m.bytes -= j.record.OutputBytes
			j.record.OutputBytes = 0
			m.setAccountingPendingLocked(j, false)
			if j.accountingUnknown {
				j.accountingUnknown = false
				m.unaccountedJobs--
				m.signal()
			}
		} else {
			// Unsafe content remains untouched. The manager stops admitting work
			// until the owner resolves it and explicitly restarts the service.
			j.stopCode = "cache_unavailable"
			m.cacheFailed = true
			for _, affected := range m.jobs {
				m.notifyLocked(affected)
			}
			m.closeErr = errors.Join(m.closeErr, err)
		}
	}
	if expired {
		// A failed cleanup still releases metadata during shutdown. Admission
		// remains fenced by cacheFailed, and closeErr preserves the failure.
		m.setAccountingPendingLocked(j, false)
		delete(m.jobs, j.record.ID)
		for index, queued := range m.queue {
			if queued == j {
				copy(m.queue[index:], m.queue[index+1:])
				m.queue[len(m.queue)-1] = nil
				m.queue = m.queue[:len(m.queue)-1]
				break
			}
		}
	}
	return expired
}

// markAccountingUnknownLocked requires mu. Neither the configured job limit
// nor the last progress report bounds bytes written since a failed inspection.
// Preserve the known charge and fence new work until guarded removal succeeds.
func (m *Manager) markAccountingUnknownLocked(j *managedJob) {
	m.setAccountingPendingLocked(j, false)
	if !j.accountingUnknown {
		j.accountingUnknown = true
		m.unaccountedJobs++
	}
}

func (m *Manager) rejectCacheObservationLocked(j *managedJob) {
	m.markAccountingUnknownLocked(j)
	if j.finished {
		m.invalidateFinishedLocked(j, "cache_unavailable")
	} else {
		m.stopLocked(j, "cache_unavailable")
	}
	m.signal()
}

func (m *Manager) setAccountingPendingLocked(j *managedJob, pending bool) {
	if j.accountingPending == pending {
		return
	}
	j.accountingPending = pending
	if pending {
		m.pendingAccountingJobs++
	} else {
		m.pendingAccountingJobs--
		m.signal()
	}
}

// Close starts an irreversible background shutdown. Even when ctx expires, the
// manager continues reaping children and waits for outstanding readers and
// diagnostic reservations before removing cache files and releasing its
// filesystem lock. It is safe to retry.
func (m *Manager) Close(ctx context.Context) error {
	m.mu.Lock()
	if !m.closing {
		m.closing = true
		m.completions.close()
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
	// Runners only hand off their existing tickets. Waiting for terminal work
	// here would deadlock a fixed worker which still needs that handoff boundary.
	m.runners.Wait()
	m.finalizers.stopAfterDrain()
	finalizerErr := m.finalizers.wait(context.Background())
	m.mu.Lock()
	m.closeErr = errors.Join(m.closeErr, finalizerErr)
	m.mu.Unlock()
	// Diagnostic owners remain independent of the transcode completion pool.
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
