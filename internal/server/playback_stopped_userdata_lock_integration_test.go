//go:build linux

package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

type stoppedUserDataLockEvidence struct {
	Marker, RunID, Scope, Failure                                                         string
	Complete, Failed                                                                      bool
	BlockerBackendPID                                                                     int
	WaiterBackendPID                                                                      int
	WaiterCount                                                                           int64
	RowHeld, RowWaitObserved, ResponsePendingWhileHeld                                    bool
	RowHeldAtObservationEnd, HeldTransactionVerified, EarlyRetirementVerified             bool
	BarrierReleased                                                                       bool
	EncoderPID                                                                            int
	EncoderStartTick                                                                      uint64
	EncoderPGID                                                                           int
	JobID                                                                                 string
	Before, BeforeStop, AfterHeldWait                                                     hlsWallPauseProcessCounters
	CPUAndCharacterIOGrowing                                                              bool
	CPUAndCharacterIOContinuedWhileHeld                                                   bool
	StopRequestedMS, RowWaitObservedMS, HeldWaitEndMS, ReleaseMS, ResponseMS              int64
	EncoderExitedBeforeRelease, EncoderReapedBeforeRelease, GroupClosedBeforeRelease      bool
	EncoderExitedAfterCleanup, EncoderReapedAfterCleanup, GroupClosedAfterCleanup         bool
	RetirementIdentityKnown                                                               bool
	StoppedStatus                                                                         int
	CommittedState                                                                        string
	CommittedRevision                                                                     int64
	StoppedRequestJoined, ConsumerJoined, ControlLaneVerified, RuntimeClosed, StoreClosed bool
	SourceDescriptorsAfterClose                                                           int
	CacheRemoved                                                                          bool
	ManagerScopeAfterClose                                                                transcode.ResourceUsage
	ProcessBefore, ProcessAfter                                                           media.ProcessCapacitySnapshot
	DataMaxConnections, ControlMaxConnections, ApplicationMaxConnections                  int32
	DataConnectionsAfterClose, ControlConnectionsAfterClose                               int32
	Boundaries                                                                            []string
}

func stoppedUserDataLockSave(t *testing.T, directory string, evidence *stoppedUserDataLockEvidence) {
	t.Helper()
	evidence.Failed = t.Failed()
	data, err := json.MarshalIndent(evidence, "", "  ")
	if err == nil && len(data) > 64<<10 {
		t.Error("stopped-row-lock evidence exceeded its fixed byte limit")
		return
	}
	if err == nil {
		err = os.WriteFile(filepath.Join(directory, "evidence.json.new"), append(data, '\n'), 0600)
	}
	if err == nil {
		err = os.Rename(filepath.Join(directory, "evidence.json.new"), filepath.Join(directory, "evidence.json"))
	}
	if err != nil {
		t.Errorf("preserve bounded stopped-row-lock evidence: error_type=%T", err)
	}
}

// This observes the same pinned encoder identity. A missing /proc entry after
// pidfd exit proves reap; a reused PID or unknown observation never proves it.
func stoppedUserDataEncoderRetirement(process playbackStopAliasEncoder) (exited, reaped, groupClosed, known bool) {
	exited, err := hlsWallPausePIDFDExited(process.pidfd)
	if err != nil {
		return false, false, false, false
	}
	_, start, statErr := hlsColdPID(process.pid)
	if statErr == nil && start != process.startTick || statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return exited, false, false, false
	}
	reaped = exited && errors.Is(statErr, os.ErrNotExist)
	if reaped {
		// The numeric group is inspected only after the original leader's
		// pidfd/start identity is known reaped; this never sends a signal.
		groupClosed = errors.Is(syscall.Kill(-process.pid, 0), syscall.ESRCH)
	}
	return exited, reaped, groupClosed, true
}

// The test adds no production hook, fake encoder or helper-returned stop claim.
// It may initially fail its early-stop assertion. Bounded cleanup releases the
// real row lock, joins the standard response and observes actual resources.
func TestHTTPPlaybackStoppedReapsActualEncoderBeforeUserDataCommit(t *testing.T) {
	runID := os.Getenv("GOBY_STOPPED_USERDATA_LOCK_RUN_ID")
	if runID == "" {
		t.Skip("GOBY_STOPPED_USERDATA_LOCK_RUN_ID explicitly admits this owned actual regression")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{7,127}$`).MatchString(runID) {
		t.Fatal("stopped-row-lock regression requires a bounded run identity")
	}
	parent := os.Getenv("GOBY_STOPPED_USERDATA_LOCK_ARTIFACTS_DIR")
	if !filepath.IsAbs(parent) || filepath.Clean(parent) != parent {
		t.Fatal("stopped-row-lock evidence parent must be a private absolute directory")
	}
	info, err := os.Lstat(parent)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 || info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("stopped-row-lock evidence parent is not private")
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		t.Fatal("stopped-row-lock evidence parent is not owned by the test user")
	}
	directory, err := os.MkdirTemp(parent, "stopped-userdata-lock-")
	if err != nil || os.Chmod(directory, 0700) != nil {
		t.Fatal("create exclusive stopped-row-lock evidence")
	}
	started := time.Now()
	evidence := &stoppedUserDataLockEvidence{Marker: "goby-stopped-userdata-lock-regression-v1", RunID: runID,
		Scope: "standard Emby Stopped with one real paced software AVC/AAC producer and a held ordinary authenticated user's item-data row",
		Boundaries: []string{
			"The production capacities remain Data12/Control4, application total16 including the catalog lease inside Data; this is row contention, not pool exhaustion.",
			"The fixture execs real FFmpeg with test-only input readrate0.5; source creation and probing are unpaced.",
			"PID/start tick/pidfd/exact executable and owned cache cwd identify the actual producer; its kernel CPU and at least one read/write character-IO counter must grow before Stopped.",
			"A real PostgreSQL FOR UPDATE holds only the exact user's item-data row; pg_blocking_pids confirms the standard Stopped transaction waits on it.",
			"The five-second retirement observation occurs while that transaction remains held and before releasing it for durable report completion.",
			"A later204 or successful post-release cleanup cannot retroactively pass early exit/reap; initial failure is retained.",
			"Process counters are cumulative and a final live sample is a lower bound; no throughput, RSS or global-host-PID claim is made.",
			"The request reproduces the standard Emby Stopped route; no original Emby player process is used.",
		}}
	t.Cleanup(func() { stoppedUserDataLockSave(t, directory, evidence) })
	defer func() { stoppedUserDataLockSave(t, directory, evidence) }()
	stoppedUserDataLockSave(t, directory, evidence)
	evidence.ProcessBefore = media.GetProcessCapacityStats()
	fixture := newPlaybackStopAliasRealFixture(t)
	f, h := fixture.control, fixture.hls
	if f.app.hls.generatedWindowsEnabled {
		t.Fatal("stopped-row-lock fixture enabled the experimental generated-window graph")
	}
	evidence.DataMaxConnections, evidence.ControlMaxConnections = f.pool.Config().MaxConns, f.control.Config().MaxConns
	evidence.ApplicationMaxConnections = evidence.DataMaxConnections + evidence.ControlMaxConnections
	if evidence.DataMaxConnections != database.DataMaxConns || evidence.ControlMaxConnections != database.PlaybackControlMaxConns ||
		evidence.ApplicationMaxConnections != database.ApplicationMaxConns {
		t.Fatal("stopped-row-lock regression changed the real database capacities")
	}
	principal, headers := f.principal(t, "normal")
	graph := h.graph(t, clientSessionHTTPLogin{id: principal.SessionID, userID: principal.User.ID, deviceID: principal.Client.DeviceID, headers: headers}, 0)
	f.app.hls.mu.Lock()
	session := f.app.hls.sessions[graph.hlsID]
	f.app.hls.mu.Unlock()
	if session == nil || session.key.scope.AuthSessionID != principal.SessionID || session.key.scope.UserID != principal.User.ID ||
		session.key.scope.PlaySessionID != graph.playID || session.key.scope.ApplicationKey {
		t.Fatal("stopped regression has no exact normal Emby producer owner")
	}
	scope := session.key.scope
	startedBody := map[string]any{"PlaySessionId": graph.playID, "ItemId": h.item.ID, "MediaSourceId": media.SourceID(h.item.ID), "PositionTicks": int64(0)}
	expectStatus(t, playbackControlRequest(f.ctx, f.handler, "/emby/Sessions/Playing", startedBody, headers), http.StatusNoContent)
	getCtx, cancelGET := context.WithCancel(f.ctx)
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		request, err := http.NewRequestWithContext(getCtx, http.MethodGet, h.server.URL+graph.children[0], nil)
		if err != nil {
			return
		}
		request.Header = headers.Clone()
		response, err := h.server.Client().Do(request)
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
		}
	}()
	t.Cleanup(func() {
		cancelGET()
		select {
		case <-consumerDone:
		case <-time.After(5 * time.Second):
			t.Error("stopped-row-lock natural consumer did not join")
		}
	})
	process := playbackStopAliasObserveEncoder(t, fixture, scope)
	evidence.EncoderPID, evidence.EncoderStartTick, evidence.JobID, evidence.Before = process.pid, process.startTick, process.jobID, process.first
	pgid, err := syscall.Getpgid(process.pid)
	if err != nil || pgid != process.pid {
		t.Fatal("actual producer does not own its expected process group")
	}
	evidence.EncoderPGID = pgid
	growthDeadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(growthDeadline) {
		counters, err := hlsWallPauseReadProcess(process.pid, process.startTick)
		if err != nil {
			t.Fatal("exact encoder disappeared before Stopped work witness")
		}
		evidence.BeforeStop = counters
		if counters.UserTicks+counters.SystemTicks > process.first.UserTicks+process.first.SystemTicks &&
			(counters.ReadChars > process.first.ReadChars || counters.WriteChars > process.first.WriteChars) {
			evidence.CPUAndCharacterIOGrowing = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !evidence.CPUAndCharacterIOGrowing {
		t.Fatal("the real encoder did not show both CPU and character-IO growth before Stopped")
	}
	lockCtx, cancelLock := context.WithTimeout(f.ctx, 25*time.Second)
	defer cancelLock()
	barrier, err := f.control.Begin(lockCtx)
	if err != nil {
		t.Fatal("begin actual userdata row barrier")
	}
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			rollbackCtx, cancelRollback := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancelRollback()
			if err := barrier.Rollback(rollbackCtx); err == nil || errors.Is(err, pgx.ErrTxClosed) {
				evidence.RowHeld = false
				evidence.BarrierReleased = true
			} else {
				evidence.Failure = "row_barrier_release_failed"
			}
			evidence.ReleaseMS = time.Since(started).Milliseconds()
		})
	}
	defer release()
	t.Cleanup(release)
	if err := barrier.QueryRow(lockCtx, "SELECT user_id FROM user_item_data WHERE user_id=$1 AND item_id=$2 FOR UPDATE", principal.User.ID, h.item.ID).Scan(new(string)); err != nil {
		t.Fatal("hold exact actual userdata row after genuine Started")
	}
	evidence.BlockerBackendPID, evidence.RowHeld = int(barrier.Conn().PgConn().PID()), true
	beforeRequest, err := hlsWallPauseReadProcess(process.pid, process.startTick)
	record, recordErr := f.app.hls.manager.Snapshot(scope, process.jobID)
	state, actualStart, stateErr := hlsColdPID(process.pid)
	if err != nil || recordErr != nil || record.State != "running" || stateErr != nil || state == 'Z' || actualStart != process.startTick {
		t.Fatal("the exact encoder was not still running immediately before Stopped")
	}
	evidence.BeforeStop = beforeRequest
	stopCtx, cancelStop := context.WithTimeout(f.ctx, 20*time.Second)
	defer cancelStop()
	tagged, counts := f.traceAcquisitions(stopCtx)
	stopDone := make(chan *httptest.ResponseRecorder, 1)
	stopJoined := make(chan struct{})
	t.Cleanup(func() {
		release()
		cancelStop()
		select {
		case <-stopJoined:
		case <-time.After(5 * time.Second):
			t.Error("stopped-row-lock standard request did not join during cleanup")
		}
	})
	evidence.StopRequestedMS = time.Since(started).Milliseconds()
	go func() {
		defer close(stopJoined)
		stopDone <- playbackControlRequest(tagged, f.handler, "/emby/Sessions/Playing/Stopped",
			map[string]any{"PlaySessionId": graph.playID, "ItemId": h.item.ID, "MediaSourceId": media.SourceID(h.item.ID)}, headers)
	}()
	var stoppedResponse *httptest.ResponseRecorder
	blockDeadline := time.Now().Add(3 * time.Second)
	blockCtx, cancelBlock := context.WithDeadline(stopCtx, blockDeadline)
	defer cancelBlock()
	for time.Now().Before(blockDeadline) {
		var waiterPID int
		var waiterCount int64
		err := f.control.QueryRow(blockCtx, `SELECT COALESCE(min(activity.pid),0),count(*) FROM pg_stat_activity activity WHERE activity.datname=current_database()
			AND $1::integer=ANY(pg_blocking_pids(activity.pid)) AND activity.wait_event_type='Lock'
			AND activity.query LIKE '%user_item_data%'`, evidence.BlockerBackendPID).Scan(&waiterPID, &waiterCount)
		if err != nil {
			evidence.Failure = "row_wait_observation_failed"
			break
		}
		if waiterCount == 1 && waiterPID != evidence.BlockerBackendPID {
			evidence.WaiterBackendPID, evidence.WaiterCount = waiterPID, waiterCount
			evidence.RowWaitObserved = true
			evidence.RowWaitObservedMS = time.Since(started).Milliseconds()
			break
		}
		select {
		case stoppedResponse = <-stopDone:
			evidence.Failure = "Stopped_returned_before_row_wait"
		default:
		}
		if stoppedResponse != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	evidence.ResponsePendingWhileHeld = stoppedResponse == nil
	stoppedUserDataLockSave(t, directory, evidence)
	if evidence.RowWaitObserved && evidence.ResponsePendingWhileHeld {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			exited, reaped, group, known := stoppedUserDataEncoderRetirement(process)
			evidence.EncoderExitedBeforeRelease, evidence.EncoderReapedBeforeRelease, evidence.GroupClosedBeforeRelease, evidence.RetirementIdentityKnown = exited, reaped, group, known
			if !known {
				evidence.Failure = "retirement_identity_unknown"
				break
			}
			if reaped && group {
				break
			}
			if counters, err := hlsWallPauseReadProcess(process.pid, process.startTick); err == nil {
				evidence.AfterHeldWait = counters
				if counters.UserTicks+counters.SystemTicks > evidence.BeforeStop.UserTicks+evidence.BeforeStop.SystemTicks &&
					(counters.ReadChars > evidence.BeforeStop.ReadChars || counters.WriteChars > evidence.BeforeStop.WriteChars) {
					evidence.CPUAndCharacterIOContinuedWhileHeld = true
				}
			}
			select {
			case stoppedResponse = <-stopDone:
				evidence.ResponsePendingWhileHeld = false
			default:
			}
			if stoppedResponse != nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		var alive int
		verifyCtx, cancelVerify := context.WithTimeout(lockCtx, time.Second)
		verifyErr := barrier.QueryRow(verifyCtx, "SELECT 1").Scan(&alive)
		cancelVerify()
		if verifyErr != nil || alive != 1 {
			evidence.RowHeld = false
			evidence.Failure = "held_transaction_was_lost"
		} else {
			evidence.HeldTransactionVerified = true
		}
	}
	select {
	case stoppedResponse = <-stopDone:
		evidence.ResponsePendingWhileHeld = false
	default:
	}
	evidence.HeldWaitEndMS = time.Since(started).Milliseconds()
	evidence.RowHeldAtObservationEnd = evidence.RowHeld
	earlyStopped := evidence.RowHeld && evidence.HeldTransactionVerified && evidence.RowWaitObserved && evidence.ResponsePendingWhileHeld && evidence.RetirementIdentityKnown &&
		evidence.EncoderExitedBeforeRelease && evidence.EncoderReapedBeforeRelease && evidence.GroupClosedBeforeRelease
	evidence.EarlyRetirementVerified = earlyStopped
	stoppedUserDataLockSave(t, directory, evidence)
	release()
	if stoppedResponse == nil {
		select {
		case stoppedResponse = <-stopDone:
		case <-time.After(8 * time.Second):
			cancelStop()
			evidence.Failure = "standard_stop_response_did_not_join"
		}
	}
	if stoppedResponse != nil {
		evidence.StoppedStatus = stoppedResponse.Code
		evidence.ResponseMS = time.Since(started).Milliseconds()
	}
	select {
	case <-stopJoined:
		evidence.StoppedRequestJoined = true
	case <-time.After(5 * time.Second):
		cancelStop()
		evidence.Failure = "standard_stop_request_did_not_join"
	}
	evidence.ControlLaneVerified = counts.dataAttempts.Load() == 0 && counts.dataSuccesses.Load() == 0 && counts.unexpectedAttempts.Load() == 0 && counts.controlAttempts.Load() > 0 && counts.controlSuccesses.Load() > 0
	snapshotCtx, cancelSnapshot := context.WithTimeout(f.ctx, 3*time.Second)
	snapshotErr := f.control.QueryRow(snapshotCtx, "SELECT state,playback_revision FROM play_sessions WHERE id=$1", graph.playID).Scan(&evidence.CommittedState, &evidence.CommittedRevision)
	cancelSnapshot()
	if snapshotErr != nil {
		evidence.Failure = "final_playback_snapshot_failed"
	}
	cancelGET()
	select {
	case <-consumerDone:
		evidence.ConsumerJoined = true
	case <-time.After(5 * time.Second):
		evidence.Failure = "consumer_did_not_join"
	}
	cleanup, cancelCleanup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelCleanup()
	if err := f.app.hls.Close(cleanup); err == nil {
		evidence.RuntimeClosed = true
	} else {
		evidence.Failure = "runtime_close_failed"
	}
	resources, ok := f.app.hls.manager.(interface {
		ResourceUsage(context.Context, transcode.Scope) (transcode.ResourceUsage, error)
	})
	if !ok {
		evidence.Failure = "scoped_resources_unavailable"
	} else {
		usage, err := resources.ResourceUsage(cleanup, scope)
		if err != nil {
			evidence.Failure = "scoped_resources_failed"
		} else {
			evidence.ManagerScopeAfterClose = usage
		}
	}
	evidence.SourceDescriptorsAfterClose = playbackStopAliasSourceFDs(t, fixture.source)
	_, cacheErr := os.Stat(filepath.Join(f.app.cfg.Transcoding.CacheDirectory, process.jobID))
	evidence.CacheRemoved = errors.Is(cacheErr, os.ErrNotExist)
	if err := f.app.Close(cleanup); err == nil {
		evidence.StoreClosed = true
	} else {
		evidence.Failure = "store_close_failed"
	}
	evidence.DataConnectionsAfterClose, evidence.ControlConnectionsAfterClose = f.pool.Stat().AcquiredConns(), f.control.Stat().AcquiredConns()
	evidence.ProcessAfter = media.GetProcessCapacityStats()
	exitedAfter, reapedAfter, groupAfter, knownAfter := stoppedUserDataEncoderRetirement(process)
	evidence.EncoderExitedAfterCleanup, evidence.EncoderReapedAfterCleanup, evidence.GroupClosedAfterCleanup = exitedAfter, reapedAfter, groupAfter
	if !knownAfter {
		evidence.Failure = "final_retirement_identity_unknown"
	}
	if !earlyStopped && evidence.Failure == "" {
		evidence.Failure = "actual_encoder_not_reaped_while_userdata_write_held"
	}
	evidence.Complete = earlyStopped && evidence.CPUAndCharacterIOGrowing && evidence.Failure == "" && evidence.BarrierReleased && evidence.StoppedRequestJoined && evidence.StoppedStatus == 204 && evidence.CommittedState == "Stopped" && evidence.ControlLaneVerified &&
		evidence.ConsumerJoined && evidence.RuntimeClosed && evidence.StoreClosed && evidence.CacheRemoved && evidence.SourceDescriptorsAfterClose == 0 &&
		evidence.ManagerScopeAfterClose == (transcode.ResourceUsage{}) && evidence.DataConnectionsAfterClose == 0 && evidence.ControlConnectionsAfterClose == 0 &&
		evidence.EncoderExitedAfterCleanup && evidence.EncoderReapedAfterCleanup && evidence.GroupClosedAfterCleanup &&
		evidence.ProcessAfter.Active == 0 && evidence.ProcessAfter.Background == 0 && evidence.ProcessAfter.Queued == 0 && evidence.ProcessAfter.RetirementUnknown == evidence.ProcessBefore.RetirementUnknown
	stoppedUserDataLockSave(t, directory, evidence)
	if !evidence.Complete {
		t.Fatalf("actual Stopped userdata-lock regression failed: early_reaped=%t row_wait=%t status=%d failure=%s; inspect private evidence", earlyStopped, evidence.RowWaitObserved, evidence.StoppedStatus, evidence.Failure)
	}
	t.Logf("standard_stopped_userdata_lock_verified=true pid=%d start_tick=%d group=%d stop_requested_ms=%d reaped_before_row_release=true released_ms=%d response_ms=%d data_max=%d control_max=%d",
		process.pid, process.startTick, pgid, evidence.StopRequestedMS, evidence.ReleaseMS, evidence.ResponseMS, f.pool.Config().MaxConns, f.control.Config().MaxConns)
}
