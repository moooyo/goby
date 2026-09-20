//go:build linux

package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

// These fixtures begin after media processing. Their deliberately nonexecutable
// inventory proves that cancellation does not reinterpret an old tool snapshot.
func queuedMediaOperationCancellationFixture(t *testing.T, f *adminMetadataHTTPFixture, actor identity.Principal, itemID, requestID, kind string) library.MediaOperation {
	t.Helper()
	target, err := f.app.library.GetMediaOperationTarget(f.ctx, actor, itemID)
	if err != nil {
		t.Fatal(err)
	}
	request := library.MediaOperationRequest{
		RequestID: requestID, Kind: kind, ItemID: itemID,
		MediaSourceID: target.MediaSourceID, SourceRevision: target.SourceRevision, StreamIndex: 7,
		MaxQueued: 8, ExecutionSnapshot: json.RawMessage(`{"fixture":true}`),
	}
	if kind == library.MediaOperationOCR {
		request.Parameters = library.MediaOperationParameters{ModelIDs: []string{"eng"}, OutputFormat: "vtt", Language: "eng"}
	}
	admission, err := f.app.library.StartMediaOperation(f.ctx, actor, request)
	if err != nil || !admission.Admitted {
		t.Fatalf("admit cancellation fixture: %v", err)
	}
	return admission.Operation
}

func readyMediaOperationCancellationFixture(t *testing.T, f *adminMetadataHTTPFixture, actor identity.Principal, requestID string) library.MediaOperation {
	t.Helper()
	op := queuedMediaOperationCancellationFixture(t, f, actor, f.itemID, requestID, library.MediaOperationRemoveSubtitle)
	work, err := f.app.library.ClaimMediaOperation(f.ctx, op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.app.library.ReadyMediaOperation(f.ctx, work, library.MediaOperationResult{
		Summary: json.RawMessage(`{}`), Journal: json.RawMessage(`{"Fixture":true}`), ResultHash: strings.Repeat("a", 64),
	}); err != nil {
		t.Fatal(err)
	}
	op, err = f.app.library.GetMediaOperation(f.ctx, actor, op.ID)
	if err != nil || op.State != "ready" {
		t.Fatalf("read ready cancellation fixture: state=%s, error=%v", op.State, err)
	}
	return op
}

type mediaOperationCancellationExecutor struct {
	files        map[string]string
	gates        map[string]<-chan struct{}
	started      chan string
	unexpected   chan string
	discardCalls atomic.Int32
}

func (e *mediaOperationCancellationExecutor) reject(action string) error {
	select {
	case e.unexpected <- action:
	default:
	}
	return fmt.Errorf("unexpected cancellation fixture action: %s", action)
}

func (e *mediaOperationCancellationExecutor) Execute(context.Context, library.MediaOperationWork, func(library.MediaOperationProgress) error) (library.MediaOperationResult, error) {
	return library.MediaOperationResult{}, e.reject("execute")
}

func (e *mediaOperationCancellationExecutor) Apply(context.Context, library.MediaOperationWork, func(library.MediaOperationProgress) error) error {
	return e.reject("apply")
}

func (e *mediaOperationCancellationExecutor) Discard(ctx context.Context, work library.MediaOperationWork) error {
	if !work.Discard || work.Operation.Kind != library.MediaOperationRemoveSubtitle || work.Operation.PublicationPhase != "none" {
		return e.reject("discard without an unpublished candidate claim")
	}
	filename, ok := e.files[work.Operation.ID]
	if !ok {
		return e.reject("discard of an unowned candidate")
	}
	e.discardCalls.Add(1)
	select {
	case e.started <- work.Operation.ID:
	case <-ctx.Done():
		return ctx.Err()
	}
	if gate := e.gates[work.Operation.ID]; gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return os.Remove(filename)
}

func newMediaOperationCancellationExecutor() *mediaOperationCancellationExecutor {
	return &mediaOperationCancellationExecutor{
		files: map[string]string{}, gates: map[string]<-chan struct{}{},
		started: make(chan string, 8), unexpected: make(chan string, 8),
	}
}

func mediaOperationCancellationGate(t *testing.T) (<-chan struct{}, func()) {
	t.Helper()
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	t.Cleanup(release)
	return gate, release
}

func writeMediaOperationCancellationCandidate(t *testing.T, directory, id string) string {
	t.Helper()
	filename := filepath.Join(directory, id+".candidate")
	if err := os.WriteFile(filename, []byte("owned unpublished candidate"), 0600); err != nil {
		t.Fatal(err)
	}
	return filename
}

func waitMediaOperationCancellationStarted(t *testing.T, executor *mediaOperationCancellationExecutor, want string) {
	t.Helper()
	select {
	case id := <-executor.started:
		if id != want {
			t.Fatalf("cleanup started for %s, want %s", id, want)
		}
	case action := <-executor.unexpected:
		t.Fatalf("unexpected executor action: %s", action)
	case <-time.After(5 * time.Second):
		t.Fatal("explicit cancellation did not start cleanup")
	}
}

func assertMediaOperationCancellationQuiet(t *testing.T, executor *mediaOperationCancellationExecutor) {
	t.Helper()
	select {
	case id := <-executor.started:
		t.Fatalf("cleanup started without an available cancellation slot: %s", id)
	case action := <-executor.unexpected:
		t.Fatalf("unavailable inventory dispatched %s", action)
	case <-time.After(150 * time.Millisecond):
	}
}

func waitMediaOperationCancelled(t *testing.T, f *adminMetadataHTTPFixture, actor identity.Principal, id string) library.MediaOperation {
	t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		op, err := f.app.library.GetMediaOperation(f.ctx, actor, id)
		if err != nil {
			t.Fatal(err)
		}
		if op.State == "cancelled" && op.WorkerToken == "" {
			if op.FinishedAt == nil || op.CancelRequestedAt == nil || op.PublicationPhase != "none" || op.Applied {
				t.Fatalf("cancelled operation lost its terminal evidence: %+v", op)
			}
			return op
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatalf("cancellation did not finish: state=%s, worker=%q, error=%s", op.State, op.WorkerToken, op.ErrorCode)
		}
	}
}

func TestDisabledMediaOperationsCancelOCRWithoutExecutionInventory(t *testing.T) {
	f, actor, ready := adminMediaOperationReadyHTTPFixture(t)
	r := f.app.mediaOperations
	if r.processingEnabled || r.configuration.Enabled || r.Available() {
		t.Fatal("cancellation fixture unexpectedly enabled media processing")
	}
	if _, err := decodeMediaOperationExecution(ready.ExecutionSnapshot); err == nil {
		t.Fatal("fixture must have a nonexecutable historical inventory")
	}
	executor := newMediaOperationCancellationExecutor()
	r.executors[library.MediaOperationOCR] = executor
	untouched := queuedMediaOperationCancellationFixture(t, f, actor, f.secondID, "untouched-queued-ocr", library.MediaOperationOCR)
	request := library.MediaOperationRequest{
		RequestID: "disabled-new-ocr", Kind: library.MediaOperationOCR, ItemID: ready.ItemID,
		MediaSourceID: ready.MediaSourceID, SourceRevision: ready.SourceRevision, StreamIndex: 7,
		Parameters: library.MediaOperationParameters{ModelIDs: []string{"eng"}, OutputFormat: "vtt", Language: "eng"},
	}
	if _, err := r.Start(f.ctx, actor, request); !errors.Is(err, library.ErrUnavailable) {
		t.Fatalf("disabled Start returned %v, want unavailable", err)
	}
	if _, err := r.Apply(f.ctx, actor, ready.ID, library.MediaOperationApplyRequest{
		RequestID: "disabled-new-apply", Revision: ready.Revision, SourceRevision: ready.SourceRevision, ResultHash: ready.ResultHash,
	}); !errors.Is(err, library.ErrUnavailable) {
		t.Fatalf("disabled Apply returned %v, want unavailable", err)
	}
	for _, state := range []string{"ready", "queued", "interrupted"} {
		t.Run(state, func(t *testing.T) {
			op := ready
			if state != "ready" {
				op = queuedMediaOperationCancellationFixture(t, f, actor, f.itemID, "cancel-ocr-"+state, library.MediaOperationOCR)
			}
			if state == "interrupted" {
				work, err := f.app.library.ClaimMediaOperation(f.ctx, op.ID)
				if err != nil {
					t.Fatal(err)
				}
				if err := f.app.library.FinishMediaOperationWork(f.ctx, work, context.Canceled, true); err != nil {
					t.Fatal(err)
				}
				op, err = f.app.library.GetMediaOperation(f.ctx, actor, op.ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			if op.State != state || !op.CanCancel {
				t.Fatalf("%s OCR did not expose cancellation: %+v", state, op)
			}
			receipt, err := r.Cancel(f.ctx, actor, op.ID, op.Revision)
			if err != nil || receipt.State != "cancelled" || receipt.WorkerToken != "" {
				t.Fatalf("cancel %s OCR without tools: state=%s, error=%v", state, receipt.State, err)
			}
			waitMediaOperationCancelled(t, f, actor, op.ID)
		})
	}
	r.Wake()
	assertMediaOperationCancellationQuiet(t, executor)
	current, err := f.app.library.GetMediaOperation(f.ctx, actor, untouched.ID)
	if err != nil || current.State != "queued" || current.WorkerToken != "" || current.Revision != untouched.Revision {
		t.Fatalf("cancellation wake started unrelated queued OCR: state=%s, error=%v", current.State, err)
	}
	var published int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM item_owned_subtitles WHERE item_id=$1`, f.itemID).Scan(&published); err != nil || published != 0 {
		t.Fatalf("disabled cancellation published a subtitle: count=%d, error=%v", published, err)
	}
}

func TestDisabledMediaOperationsDiscardQueueUsesCompletionEvents(t *testing.T) {
	f, actor, _ := adminMediaOperationReadyHTTPFixture(t)
	first := readyMediaOperationCancellationFixture(t, f, actor, "first-candidate")
	second := readyMediaOperationCancellationFixture(t, f, actor, "second-candidate")
	source, err := os.ReadFile(f.mediaPath)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err := f.app.mediaOperations.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE media_operations SET cancel_requested_at=clock_timestamp(),revision=revision+1 WHERE id=$1`, first.ID); err != nil {
		t.Fatal(err)
	}
	r, err := newMediaOperationsRuntime(f.ctx, f.app)
	if err != nil {
		t.Fatal(err)
	}
	f.app.mediaOperations = r
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := r.Close(ctx); err != nil {
			t.Errorf("close restarted cancellation runtime: %v", err)
		}
	})
	if r.processingEnabled || r.configuration.MaxConcurrent != 0 {
		t.Fatal("fixture must exercise the dormant coordinator's cleanup capacity")
	}
	executor := newMediaOperationCancellationExecutor()
	executor.files[first.ID] = writeMediaOperationCancellationCandidate(t, directory, first.ID)
	executor.files[second.ID] = writeMediaOperationCancellationCandidate(t, directory, second.ID)
	firstGate, releaseFirst := mediaOperationCancellationGate(t)
	secondGate, releaseSecond := mediaOperationCancellationGate(t)
	executor.gates[first.ID], executor.gates[second.ID] = firstGate, secondGate
	r.executors[library.MediaOperationRemoveSubtitle] = executor
	for _, id := range []string{first.ID, second.ID} {
		op, err := f.app.library.GetMediaOperation(f.ctx, actor, id)
		if err != nil || op.State != "ready" || op.CancelRequestedAt != nil || op.WorkerToken != "" {
			t.Fatalf("startup retained a runnable cancellation grant: state=%s, error=%v", op.State, err)
		}
		if id == first.ID {
			first = op
		} else {
			second = op
		}
	}
	r.Wake()
	assertMediaOperationCancellationQuiet(t, executor)
	for _, filename := range executor.files {
		if _, err := os.Stat(filename); err != nil {
			t.Fatalf("startup or ordinary wake removed an unpublished candidate: %v", err)
		}
	}
	if _, err := r.Cancel(f.ctx, actor, first.ID, first.Revision); err != nil {
		t.Fatal(err)
	}
	waitMediaOperationCancellationStarted(t, executor, first.ID)
	if _, err := r.Cancel(f.ctx, actor, second.ID, second.Revision); err != nil {
		t.Fatal(err)
	}
	assertMediaOperationCancellationQuiet(t, executor)
	releaseFirst()
	// No subsequent caller wake or polling task may be needed to free the slot
	// and begin the next candidate's cleanup.
	waitMediaOperationCancellationStarted(t, executor, second.ID)
	waitMediaOperationCancelled(t, f, actor, first.ID)
	releaseSecond()
	waitMediaOperationCancelled(t, f, actor, second.ID)
	if executor.discardCalls.Load() != 2 {
		t.Fatalf("discard calls=%d, want one per candidate", executor.discardCalls.Load())
	}
	for _, filename := range executor.files {
		if _, err := os.Stat(filename); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("cancelled candidate still exists: %v", err)
		}
	}
	current, err := os.ReadFile(f.mediaPath)
	if err != nil || !bytes.Equal(current, source) {
		t.Fatalf("cancellation changed the original media: %v", err)
	}
	assertMediaOperationCancellationQuiet(t, executor)
}

func TestDisabledMediaOperationsRetryFinishWithoutRepeatingDiscard(t *testing.T) {
	f, actor, _ := adminMediaOperationReadyHTTPFixture(t)
	op := readyMediaOperationCancellationFixture(t, f, actor, "retry-candidate-finish")
	r := f.app.mediaOperations
	executor := newMediaOperationCancellationExecutor()
	filename := writeMediaOperationCancellationCandidate(t, t.TempDir(), op.ID)
	executor.files[op.ID] = filename
	r.executors[library.MediaOperationRemoveSubtitle] = executor
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := r.Close(ctx); err != nil {
			t.Errorf("close cancellation retry runtime: %v", err)
		}
	})
	gate, err := f.pool.Acquire(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	var gatePID int32
	if err := gate.QueryRow(f.ctx, `SELECT pg_backend_pid()`).Scan(&gatePID); err != nil {
		gate.Release()
		t.Fatal(err)
	}
	if _, err := gate.Exec(f.ctx, `SELECT pg_advisory_lock(21444,$1)`, gatePID); err != nil {
		gate.Release()
		t.Fatal(err)
	}
	var releaseOnce sync.Once
	release := func() {
		releaseOnce.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := gate.Exec(ctx, `SELECT pg_advisory_unlock(21444,$1)`, gatePID); err != nil {
				t.Errorf("release cancellation finish gate: %v", err)
			}
			gate.Release()
		})
	}
	defer release()
	for _, statement := range []string{
		`CREATE SEQUENCE media_operation_cancel_finish_attempts`,
		`CREATE FUNCTION retry_media_cancel_finish() RETURNS trigger LANGUAGE plpgsql AS $$
		DECLARE attempt bigint;
		BEGIN
		    attempt := nextval('media_operation_cancel_finish_attempts');
		    IF attempt = 1 THEN
		        RAISE EXCEPTION USING ERRCODE='P0001', MESSAGE='Injected cancellation finish failure';
		    END IF;
		    PERFORM pg_advisory_xact_lock(21444,TG_ARGV[0]::integer);
		    RETURN NEW;
		END;
		$$`,
		fmt.Sprintf(`CREATE CONSTRAINT TRIGGER retry_media_cancel_finish
		    AFTER UPDATE ON media_operations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
		    WHEN (NEW.state='cancelled' AND OLD.state IS DISTINCT FROM NEW.state
		          AND OLD.worker_token<>'' AND NEW.worker_token='')
		    EXECUTE FUNCTION retry_media_cancel_finish('%d')`, gatePID),
	} {
		if _, err := f.pool.Exec(f.ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Cancel(f.ctx, actor, op.ID, op.Revision); err != nil {
		t.Fatal(err)
	}
	waitMediaOperationCancellationStarted(t, executor, op.ID)
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		var attempts int64
		var called bool
		if err := f.pool.QueryRow(f.ctx, `SELECT last_value,is_called FROM media_operation_cancel_finish_attempts`).Scan(&attempts, &called); err != nil {
			t.Fatal(err)
		}
		if called && attempts == 2 {
			break
		}
		if attempts > 2 {
			t.Fatalf("finish retried unexpectedly while the second commit was blocked: %d", attempts)
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("failed finish was not retried without another external wake")
		}
	}
	current, err := f.app.library.GetMediaOperation(f.ctx, actor, op.ID)
	if err != nil || current.State != "ready" || current.WorkerToken == "" {
		t.Fatalf("failed commit did not preserve its claimed cleanup: state=%s, error=%v", current.State, err)
	}
	if executor.discardCalls.Load() != 1 {
		t.Fatalf("SQL retry repeated filesystem cleanup: %d calls", executor.discardCalls.Load())
	}
	if _, err := os.Stat(filename); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cleanup had not removed its owned candidate before retrying SQL: %v", err)
	}
	release()
	waitMediaOperationCancelled(t, f, actor, op.ID)
	var attempts int64
	if err := f.pool.QueryRow(f.ctx, `SELECT last_value FROM media_operation_cancel_finish_attempts`).Scan(&attempts); err != nil || attempts != 2 {
		t.Fatalf("finish attempts=%d, want exactly two: %v", attempts, err)
	}
	if executor.discardCalls.Load() != 1 {
		t.Fatal("successful retry ran candidate cleanup again")
	}
	assertMediaOperationCancellationQuiet(t, executor)
}

func TestUnavailableMediaOperationsInventoryStillDiscardsCandidate(t *testing.T) {
	f, actor, _ := adminMediaOperationReadyHTTPFixture(t)
	op := readyMediaOperationCancellationFixture(t, f, actor, "missing-tools-candidate")
	directory := t.TempDir()
	filename := writeMediaOperationCancellationCandidate(t, directory, op.ID)
	if err := f.app.mediaOperations.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	f.app.cfg.MediaOperations = config.MediaOperationsConfig{
		Enabled: true, MaxConcurrent: 1, MaxQueued: 1, MaxRuntimeSeconds: 90,
		MaxScratchBytes: 64 << 20, ScratchDirectory: filepath.ToSlash(directory), WritableProfiles: []string{"matroska-v1"},
	}
	f.app.cfg.FFmpegPath = filepath.Join(directory, "missing-ffmpeg")
	f.app.cfg.FFprobePath = filepath.Join(directory, "missing-ffprobe")
	r, err := newMediaOperationsRuntime(f.ctx, f.app)
	if err != nil {
		t.Fatal(err)
	}
	f.app.mediaOperations = r
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := r.Close(ctx); err != nil {
			t.Errorf("close unavailable-inventory cancellation runtime: %v", err)
		}
	})
	if !r.configuration.Enabled || r.processingEnabled || r.Available() {
		t.Fatal("missing executables did not produce an enabled but unavailable inventory")
	}
	executor := newMediaOperationCancellationExecutor()
	executor.files[op.ID] = filename
	r.executors[library.MediaOperationRemoveSubtitle] = executor
	if _, err := r.Cancel(f.ctx, actor, op.ID, op.Revision); err != nil {
		t.Fatal(err)
	}
	waitMediaOperationCancellationStarted(t, executor, op.ID)
	waitMediaOperationCancelled(t, f, actor, op.ID)
	if executor.discardCalls.Load() != 1 {
		t.Fatalf("unavailable-inventory discard calls=%d, want one", executor.discardCalls.Load())
	}
	if _, err := os.Stat(filename); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing execution tools prevented candidate cleanup: %v", err)
	}
	assertMediaOperationCancellationQuiet(t, executor)
}

func TestDisabledMediaOperationsCancelUnclaimedPreparedRecovery(t *testing.T) {
	f, actor, _ := adminMediaOperationReadyHTTPFixture(t)
	op := readyMediaOperationCancellationFixture(t, f, actor, "prepared-recovery-cancel")
	executor := newMediaOperationCancellationExecutor()
	f.app.mediaOperations.executors[library.MediaOperationRemoveSubtitle] = executor
	if _, err := f.pool.Exec(f.ctx, `UPDATE media_operations SET state='recovery_required',publication_phase='prepared',revision=revision+1 WHERE id=$1`, op.ID); err != nil {
		t.Fatal(err)
	}
	op, err := f.app.library.GetMediaOperation(f.ctx, actor, op.ID)
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := f.app.library.RecoverMediaOperation(f.ctx, actor, op.ID, library.MediaOperationApplyRequest{
		RequestID: "explicit-prepared-recovery", Revision: op.Revision, SourceRevision: op.SourceRevision, ResultHash: op.ResultHash,
	})
	if err != nil || !recovery.Admitted || recovery.Operation.State != "applying" || recovery.Operation.WorkerToken != "" {
		t.Fatalf("admit prepared recovery before claim: %+v, %v", recovery, err)
	}
	cancelled, err := f.app.mediaOperations.Cancel(f.ctx, actor, op.ID, recovery.Operation.Revision)
	if err != nil || cancelled.State != "recovery_required" || cancelled.PublicationPhase != "prepared" || !cancelled.CanRecover || cancelled.WorkerToken != "" {
		t.Fatalf("cancelled recovery lost its durable publication barrier: %+v, %v", cancelled, err)
	}
	var sameJournal bool
	if err := f.pool.QueryRow(f.ctx, `SELECT journal=$2::jsonb FROM media_operations WHERE id=$1`, op.ID, op.Journal).Scan(&sameJournal); err != nil || !sameJournal {
		t.Fatalf("cancelling unclaimed recovery changed its journal: %v", err)
	}
	pending, err := f.app.library.PendingMediaOperationCancellations(f.ctx, 1)
	if err != nil || len(pending) != 0 {
		t.Fatalf("prepared recovery remained at the cleanup queue head: %d entries, %v", len(pending), err)
	}
	assertMediaOperationCancellationQuiet(t, executor)
}
