package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

const (
	mediaDiagnosticRetention     = 30 * time.Minute
	mediaDiagnosticTokenLifetime = 5 * time.Minute
	mediaDiagnosticRunLifetime   = 2 * time.Minute
	mediaDiagnosticMaxRuns       = 32
)

type mediaDiagnosticAPIError struct {
	status        int
	code, message string
}

func (err *mediaDiagnosticAPIError) Error() string { return err.code }

var (
	errMediaDiagnosticUnavailable = &mediaDiagnosticAPIError{503, "diagnostics_unavailable", "Media diagnostics are unavailable in this server instance."}
	errMediaDiagnosticInstance    = &mediaDiagnosticAPIError{409, "instance_changed", "The server instance changed. Refresh diagnostics before starting a new run."}
	errMediaDiagnosticConflict    = &mediaDiagnosticAPIError{409, "request_conflict", "This identifier belongs to a different diagnostic request."}
	errMediaDiagnosticExpired     = &mediaDiagnosticAPIError{409, "request_expired", "The start request expired. Check the previous outcome before starting a new diagnostic."}
	errMediaDiagnosticBusy        = &mediaDiagnosticAPIError{409, "diagnostic_busy", "Another diagnostic is active or all conversion slots are occupied."}
	errMediaDiagnosticRetention   = &mediaDiagnosticAPIError{409, "retention_full", "Diagnostic history is full. Wait for an older result to expire."}
	errMediaDiagnosticNotFound    = &mediaDiagnosticAPIError{404, "diagnostic_run_not_found", "No retained diagnostic result was found for this request."}
)

type mediaDiagnosticOwner interface {
	Run(media.DiagnosticSelection, func(context.Context) error, func(media.DiagnosticReport)) (media.DiagnosticReport, error)
	Cancel()
	Close() error
}

type mediaDiagnosticStart struct{ InstanceId, RequestId, StartToken, Mode string }

type mediaDiagnosticRun struct {
	id, mode, state, code string
	revision              uint64
	created, updated      time.Time
	finished              *time.Time
	fingerprint           [32]byte
	actor                 identity.Principal
	report                *media.DiagnosticReport
	ctx                   context.Context
	cancel                context.CancelFunc
	cancelRequested       bool
	authorityCode         string
}

// History is bounded to this generation and is not persisted in playback or
// scheduled-task tables. A signed short-lived start token prevents an identical
// old request from being admitted after its retained result expires. Instance
// identity rejects old requests after a process/generation restart.
type mediaDiagnosticRuntime struct {
	mu        sync.Mutex
	ctx       context.Context
	cancel    context.CancelFunc
	instance  string
	key       [32]byte
	born      time.Time
	now       func() time.Time
	enabled   bool
	profile   media.DiagnosticProfile
	options   media.DiagnosticExecutionOptions
	runs      map[string]*mediaDiagnosticRun
	order     []string
	active    string
	closing   bool
	wg        sync.WaitGroup
	done      chan struct{}
	once      sync.Once
	authorize func(context.Context, identity.Principal) error
	reserve   func() (func(), error)
	open      func(context.Context, media.DiagnosticExecutionOptions) (mediaDiagnosticOwner, error)
}

func newMediaDiagnosticRuntime(s *Server) (*mediaDiagnosticRuntime, error) {
	var random [48]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, errMediaDiagnosticUnavailable
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &mediaDiagnosticRuntime{ctx: ctx, cancel: cancel, instance: hex.EncodeToString(random[:16]), born: time.Now(), now: time.Now,
		enabled: s.cfg.MediaDiagnostics.Enabled, options: s.cfg.MediaDiagnostics.ExecutionOptions(s.cfg.FFmpegPath),
		profile: media.DiagnosticProfile{Decode: s.cfg.Transcoding.Hardware.Decode, Encode: s.cfg.Transcoding.Hardware.Encode, Device: s.cfg.Transcoding.Hardware.Device},
		runs:    make(map[string]*mediaDiagnosticRun), done: make(chan struct{}), authorize: s.checkMediaDiagnosticActor, reserve: s.reserveMediaDiagnostic}
	copy(m.key[:], random[16:])
	m.open = func(ctx context.Context, options media.DiagnosticExecutionOptions) (mediaDiagnosticOwner, error) {
		owner, err := media.NewDiagnosticExecution(ctx, options)
		if owner == nil {
			return nil, err
		}
		return owner, err
	}
	return m, nil
}

func (s *Server) checkMediaDiagnosticActor(ctx context.Context, actor identity.Principal) error {
	if s.db == nil {
		return media.ErrDiagnosticAuthority
	}
	bounded, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	tx, err := s.db.BeginTx(bounded, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer tx.Rollback(bounded)
	if err := identity.CheckAdministrator(bounded, tx, actor, identity.AdministratorNative, false); err != nil {
		return err
	}
	return tx.Commit(bounded)
}

func (s *Server) reserveMediaDiagnostic() (func(), error) {
	if s.hls == nil {
		if s.cfg.Transcoding.Enabled {
			return nil, errMediaDiagnosticUnavailable
		}
		return func() {}, nil
	}
	reserver, ok := s.hls.manager.(interface{ ReserveDiagnostic() (func(), error) })
	if !ok {
		return nil, errMediaDiagnosticUnavailable
	}
	release, err := reserver.ReserveDiagnostic()
	if errors.Is(err, transcode.ErrBusy) {
		return nil, errMediaDiagnosticBusy
	}
	if err != nil {
		return nil, errMediaDiagnosticUnavailable
	}
	return release, nil
}

func (m *mediaDiagnosticRuntime) token(actor identity.Principal, now time.Time) (string, time.Time) {
	// Sub uses the process-local monotonic clock. Wall-clock rollback cannot
	// make an expired request usable after its history entry has been purged.
	issued := strconv.FormatInt(now.Sub(m.born).Nanoseconds(), 10)
	mac := hmac.New(sha256.New, m.key[:])
	_, _ = mac.Write([]byte(m.instance + "\x00" + actor.User.ID + "\x00" + actor.SessionID + "\x00" + issued))
	return issued + "." + hex.EncodeToString(mac.Sum(nil)), now.Add(mediaDiagnosticTokenLifetime).UTC()
}

func (m *mediaDiagnosticRuntime) validToken(actor identity.Principal, token string, now time.Time) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 2 || len(parts[0]) > 19 || len(parts[1]) != 64 {
		return false
	}
	issued, err := strconv.ParseInt(parts[0], 10, 64)
	elapsed := now.Sub(m.born).Nanoseconds()
	if err != nil || issued < 0 || strconv.FormatInt(issued, 10) != parts[0] || elapsed < issued || elapsed-issued >= mediaDiagnosticTokenLifetime.Nanoseconds() {
		return false
	}
	expected, _ := m.token(actor, m.born.Add(time.Duration(issued)))
	return hmac.Equal([]byte(expected), []byte(token))
}

func diagnosticRequestFingerprint(actor identity.Principal, request mediaDiagnosticStart) [32]byte {
	data, _ := json.Marshal([]string{actor.User.ID, actor.SessionID, request.InstanceId, request.RequestId, request.StartToken, request.Mode})
	return sha256.Sum256(data)
}

func (m *mediaDiagnosticRuntime) purge(now time.Time) {
	kept := m.order[:0]
	for _, id := range m.order {
		run := m.runs[id]
		if run.finished != nil && now.Sub(run.created) >= mediaDiagnosticRetention {
			delete(m.runs, id)
		} else {
			kept = append(kept, id)
		}
	}
	m.order = kept
}

func (m *mediaDiagnosticRuntime) start(ctx context.Context, actor identity.Principal, request mediaDiagnosticStart) (map[string]any, bool, error) {
	if m == nil {
		return nil, false, errMediaDiagnosticUnavailable
	}
	if actor.Kind != "admin" || actor.IsApplicationKey() || actor.ApplicationKeyID != 0 || actor.ClientSessionID != "" || actor.User.ID == "" || actor.SessionID == "" {
		return nil, false, identity.ErrUnauthorized
	}
	if !diagnosticHexID(request.InstanceId) || !diagnosticHexID(request.RequestId) || len(request.StartToken) > 128 || request.Mode != "software" && request.Mode != "configured" {
		return nil, false, errMediaDiagnosticConflict
	}
	if err := m.authorize(ctx, actor); err != nil {
		return nil, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.purge(now)
	if request.InstanceId != m.instance {
		return nil, false, errMediaDiagnosticInstance
	}
	fingerprint := diagnosticRequestFingerprint(actor, request)
	if existing := m.runs[request.RequestId]; existing != nil {
		if existing.fingerprint != fingerprint {
			return nil, false, errMediaDiagnosticConflict
		}
		return m.detail(existing), false, nil
	}
	if !m.validToken(actor, request.StartToken, now) {
		return nil, false, errMediaDiagnosticExpired
	}
	if m.closing || !m.enabled {
		return nil, false, errMediaDiagnosticUnavailable
	}
	if request.Mode != "software" && request.Mode != "configured" {
		return nil, false, errMediaDiagnosticConflict
	}
	if request.Mode == "configured" && !m.hardwareConfigured() {
		return nil, false, errMediaDiagnosticUnavailable
	}
	if m.active != "" {
		return nil, false, errMediaDiagnosticBusy
	}
	if len(m.runs) >= mediaDiagnosticMaxRuns {
		return nil, false, errMediaDiagnosticRetention
	}
	release, err := m.reserve()
	if err != nil {
		return nil, false, err
	}
	if release == nil {
		return nil, false, errMediaDiagnosticUnavailable
	}
	// The conversion mutex may have delayed reservation. Recheck the token's
	// monotonic deadline before admission so a delayed old POST cannot start
	// after the client has established that its request window expired.
	now = m.now()
	if err := ctx.Err(); err != nil {
		release()
		return nil, false, err
	}
	if !m.validToken(actor, request.StartToken, now) {
		release()
		return nil, false, errMediaDiagnosticExpired
	}
	lifetime, cancel := context.WithTimeout(m.ctx, mediaDiagnosticRunLifetime)
	run := &mediaDiagnosticRun{id: request.RequestId, mode: request.Mode, state: "queued", revision: 1, created: now, updated: now,
		fingerprint: fingerprint, actor: actor, ctx: lifetime, cancel: cancel}
	m.runs[run.id], m.active = run, run.id
	m.order = append(m.order, run.id)
	m.wg.Add(1)
	initial := m.detail(run)
	go m.work(run, release)
	return initial, true, nil
}

func (m *mediaDiagnosticRuntime) hardwareConfigured() bool {
	return m.profile.Decode != "" && m.profile.Decode != "software" || m.profile.Encode != "" && m.profile.Encode != "software"
}

func (m *mediaDiagnosticRuntime) authority(ctx context.Context, run *mediaDiagnosticRun) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := m.authorize(ctx, run.actor); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		code := "diagnostic_authority_unavailable"
		if errors.Is(err, identity.ErrUnauthorized) || errors.Is(err, media.ErrDiagnosticAuthority) {
			code = "diagnostic_authority_lost"
		}
		m.mu.Lock()
		run.authorityCode = code
		m.mu.Unlock()
		run.cancel()
		return media.ErrDiagnosticAuthority
	}
	return ctx.Err()
}

func (m *mediaDiagnosticRuntime) watchAuthority(run *mediaDiagnosticRun, done chan<- struct{}) {
	defer close(done)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-run.ctx.Done():
			return
		case <-ticker.C:
			if m.authority(run.ctx, run) != nil {
				return
			}
		}
	}
}

func (m *mediaDiagnosticRuntime) work(run *mediaDiagnosticRun, release func()) {
	defer m.wg.Done()
	watchDone := make(chan struct{})
	go m.watchAuthority(run, watchDone)
	err := m.authority(run.ctx, run)
	var owner mediaDiagnosticOwner
	var report *media.DiagnosticReport
	if err == nil {
		m.mu.Lock()
		if run.cancelRequested || m.closing {
			err = context.Canceled
		} else if run.authorityCode != "" {
			err = media.ErrDiagnosticAuthority
		} else {
			run.state, run.updated = "running", m.now()
			run.revision++
		}
		m.mu.Unlock()
		if err == nil {
			owner, err = m.open(run.ctx, m.options)
		}
	}
	if err == nil && owner == nil {
		err = media.ErrDiagnosticResources
	}
	if err == nil {
		selection := media.DiagnosticSelection{}
		if run.mode == "configured" {
			selection.IncludeConfiguredHardware, selection.ConfiguredProfile = true, m.profile
		}
		value, runErr := owner.Run(selection, func(ctx context.Context) error { return m.authority(ctx, run) }, func(value media.DiagnosticReport) {
			// The pipeline supplies detached snapshots. Never mutate a published
			// report after releasing the lock; HTTP readers may still encode it.
			m.mu.Lock()
			run.report, run.updated = &value, m.now()
			run.revision++
			m.mu.Unlock()
		})
		report, err = &value, runErr
	}
	run.cancel()
	<-watchDone
	if owner != nil {
		for {
			if closeErr := owner.Close(); closeErr == nil {
				break
			}
			m.mu.Lock()
			run.state, run.code, run.updated = "cleanup_pending", "diagnostic_process_closure_failed", m.now()
			run.revision++
			run.report = report
			m.mu.Unlock()
			// This retains the same owner and conversion reservation. It never
			// starts another command or substitutes a new resource scope.
			time.Sleep(time.Second)
		}
	}
	m.mu.Lock()
	checkFinal := run.authorityCode == "" && !run.cancelRequested && !m.closing && report != nil && report.State == "stages_complete"
	m.mu.Unlock()
	if checkFinal {
		if finalErr := m.authority(m.ctx, run); finalErr != nil {
			err = finalErr
		}
	}
	// Only now are the child processes and all session resources closed.
	release()
	m.mu.Lock()
	defer m.mu.Unlock()
	state, code := "failed", "diagnostic_execution_failed"
	if report != nil {
		value := *report
		value.SessionClosureRequired = false
		switch value.State {
		case "stages_complete":
			value.State = "passed"
		case "incomplete":
			value.State = "failed"
		}
		report = &value
		state, code = value.State, value.Code
	}
	if report == nil && errors.Is(err, media.ErrDiagnosticResources) {
		state, code = "unavailable", "diagnostic_resources_unavailable"
	}
	if report == nil && errors.Is(err, media.ErrDiagnosticTool) {
		state, code = "unavailable", "diagnostic_tool_unavailable"
	}
	if errors.Is(err, context.Canceled) || run.cancelRequested || m.closing {
		state, code = "cancelled", "diagnostic_cancelled"
	}
	if run.authorityCode != "" {
		state, code = "cancelled", run.authorityCode
	}
	if errors.Is(err, context.DeadlineExceeded) {
		state, code = "failed", "diagnostic_deadline_exceeded"
	}
	if err != nil && state == "passed" {
		state, code = "failed", "diagnostic_execution_failed"
	}
	if state != "passed" && state != "cancelled" && state != "unavailable" {
		state = "failed"
	}
	if state == "failed" && code == "" {
		code = "diagnostic_execution_failed"
	}
	if report != nil {
		value := *report
		value.State, value.Code = state, code
		report = &value
	}
	finished := m.now()
	run.report, run.state, run.code, run.updated, run.finished = report, state, code, finished, &finished
	run.revision++
	m.active = ""
}

func (m *mediaDiagnosticRuntime) summary(run *mediaDiagnosticRun) map[string]any {
	var finished any
	if run.finished != nil {
		finished = run.finished.UTC()
	}
	return map[string]any{"Id": run.id, "InstanceId": m.instance, "Revision": strconv.FormatUint(run.revision, 10), "Mode": run.mode, "State": run.state, "Code": run.code,
		"CreatedAt": run.created.UTC(), "UpdatedAt": run.updated.UTC(), "FinishedAt": finished}
}

func (m *mediaDiagnosticRuntime) detail(run *mediaDiagnosticRun) map[string]any {
	result := m.summary(run)
	result["Report"] = run.report
	return result
}

func (m *mediaDiagnosticRuntime) status(actor identity.Principal) map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.purge(now)
	token, expires := m.token(actor, now)
	items := make([]map[string]any, 0, len(m.order))
	for index := len(m.order) - 1; index >= 0; index-- {
		items = append(items, m.summary(m.runs[m.order[index]]))
	}
	reason := ""
	if !m.enabled {
		reason = "diagnostics_not_configured"
	} else if m.closing {
		reason = "server_closing"
	}
	return map[string]any{"InstanceId": m.instance, "StartToken": token, "StartTokenExpiresAt": expires, "Available": m.enabled && !m.closing,
		"UnavailableReason": reason, "HardwareConfigured": m.hardwareConfigured(), "RetentionSeconds": int(mediaDiagnosticRetention.Seconds()), "MaxRetainedRuns": mediaDiagnosticMaxRuns, "Items": items}
}

func (m *mediaDiagnosticRuntime) get(instance, id string, cancel bool) (map[string]any, error) {
	if m == nil {
		return nil, errMediaDiagnosticUnavailable
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.purge(m.now())
	if instance != m.instance {
		return nil, errMediaDiagnosticInstance
	}
	run := m.runs[id]
	if run == nil {
		return nil, errMediaDiagnosticNotFound
	}
	if cancel && run.finished == nil {
		run.cancelRequested = true
		if run.state != "cleanup_pending" {
			run.state = "cancelling"
		}
		run.updated = m.now()
		run.cancel()
		run.revision++
	}
	return m.detail(run), nil
}

func (m *mediaDiagnosticRuntime) BeginClose() {
	if m == nil {
		return
	}
	m.once.Do(func() {
		m.mu.Lock()
		m.closing = true
		if run := m.runs[m.active]; run != nil && run.finished == nil && run.state != "cleanup_pending" {
			run.state, run.updated = "cancelling", m.now()
			run.revision++
		}
		m.cancel()
		m.mu.Unlock()
		go func() { m.wg.Wait(); close(m.done) }()
	})
}

func (m *mediaDiagnosticRuntime) cancelActor(userID, sessionID string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	run := m.runs[m.active]
	if run == nil || run.finished != nil || (userID == "" || run.actor.User.ID != userID) && (sessionID == "" || run.actor.SessionID != sessionID) {
		return
	}
	run.authorityCode = "diagnostic_authority_lost"
	if run.state != "cleanup_pending" {
		run.state = "cancelling"
	}
	run.updated = m.now()
	run.revision++
	run.cancel()
}

func (m *mediaDiagnosticRuntime) Close(ctx context.Context) error {
	if m == nil {
		return nil
	}
	m.BeginClose()
	select {
	case <-m.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
