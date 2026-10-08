//go:build linux

package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/library"
)

type mediaOperationCompensationProjectionExecutor struct {
	apply   func(library.MediaOperationWork) error
	discard func(library.MediaOperationWork) error
}

func (*mediaOperationCompensationProjectionExecutor) Execute(context.Context, library.MediaOperationWork, func(library.MediaOperationProgress) error) (library.MediaOperationResult, error) {
	return library.MediaOperationResult{}, errors.New("unexpected execution in apply compensation fixture")
}

func (executor *mediaOperationCompensationProjectionExecutor) Apply(_ context.Context, work library.MediaOperationWork, _ func(library.MediaOperationProgress) error) error {
	return executor.apply(work)
}

func (executor *mediaOperationCompensationProjectionExecutor) Discard(_ context.Context, work library.MediaOperationWork) error {
	return executor.discard(work)
}

func TestMediaOperationApplyCancellationCompensationReadsCurrentJournal(t *testing.T) {
	for _, scenario := range []string{"current", "revoked", "worker_replaced"} {
		t.Run(scenario, func(t *testing.T) {
			f, actor, _ := adminMediaOperationReadyHTTPFixture(t)
			op := readyMediaOperationCancellationFixture(t, f, actor, "projection-compensation")
			request := library.MediaOperationApplyRequest{RequestID: "projection-apply", Revision: op.Revision,
				SourceRevision: op.SourceRevision, ResultHash: op.ResultHash}
			if _, err := f.app.library.ApplyMediaOperation(f.ctx, actor, op.ID, request); err != nil {
				t.Fatal(err)
			}
			work, err := f.app.library.ClaimMediaOperation(f.ctx, op.ID)
			if err != nil {
				t.Fatal(err)
			}
			// This direct apply fixture needs a valid captured policy but starts
			// after inventory verification and never invokes an external tool.
			work.Operation.ExecutionSnapshot, err = json.Marshal(mediaOperationExecutionSnapshot{Configuration: config.MediaOperationsConfig{
				Enabled: true, MaxConcurrent: 1, MaxQueued: 1, MaxRuntimeSeconds: 60,
				MaxScratchBytes: 1 << 20, ScratchDirectory: "/fixture/media-compensation",
			}})
			if err != nil {
				t.Fatal(err)
			}
			discarded := false
			executor := &mediaOperationCompensationProjectionExecutor{
				apply: func(current library.MediaOperationWork) error {
					if _, err := f.pool.Exec(f.ctx, `UPDATE media_operations SET journal='{"Candidate":"current-retained-candidate"}'::jsonb WHERE id=$1`, op.ID); err != nil {
						return err
					}
					if _, err := f.app.library.CancelMediaOperation(f.ctx, actor, op.ID, current.Operation.Revision); err != nil {
						return err
					}
					if scenario == "revoked" {
						if _, err := f.pool.Exec(f.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, actor.SessionID); err != nil {
							return err
						}
					}
					if scenario == "worker_replaced" {
						if _, err := f.pool.Exec(f.ctx, `UPDATE media_operations SET worker_token=$2 WHERE id=$1`, op.ID, strings.Repeat("b", 32)); err != nil {
							return err
						}
					}
					return context.Canceled
				},
				discard: func(current library.MediaOperationWork) error {
					var journal struct{ Candidate string }
					if !current.Discard || current.Token != work.Token || current.Operation.WorkerToken != work.Token ||
						current.Operation.CancelRequestedAt == nil || json.Unmarshal(current.Operation.Journal, &journal) != nil || journal.Candidate != "current-retained-candidate" {
						return errors.New("compensation lost its current retained journal or owned cancellation")
					}
					discarded = true
					return nil
				},
			}
			runtime := &mediaOperationsRuntime{store: f.app.library, executors: map[string]mediaOperationExecutor{library.MediaOperationRemoveSubtitle: executor}}
			err = runtime.execute(f.ctx, work)
			if !errors.Is(err, context.Canceled) || errors.Is(err, library.ErrMediaOperationRecovery) || discarded != (scenario != "worker_replaced") {
				t.Fatalf("apply cancellation changed compensation ownership: discarded=%v, error=%v", discarded, err)
			}
		})
	}
}

func TestInactiveMediaOperationApplyReceiptRetainsPrivateFingerprint(t *testing.T) {
	f, actor, op := adminMediaOperationReadyHTTPFixture(t)
	request := library.MediaOperationApplyRequest{RequestID: "projection-inactive-receipt", Revision: op.Revision,
		SourceRevision: op.SourceRevision, ResultHash: op.ResultHash}
	if _, err := f.app.library.ApplyMediaOperation(f.ctx, actor, op.ID, request); err != nil {
		t.Fatal(err)
	}
	runtime := &mediaOperationsRuntime{store: f.app.library}
	receipt, err := runtime.inactiveApplyReceipt(f.ctx, actor, op.ID, request, false)
	if err != nil || receipt.Admitted || receipt.Operation.ApplyRequestID != request.RequestID || len(receipt.Operation.ApplyFingerprint) != 32 {
		t.Fatalf("inactive inventory lost its admitted apply receipt: %v", err)
	}
	request.ResultHash = strings.Repeat("b", 64)
	if _, err := runtime.inactiveApplyReceipt(f.ctx, actor, op.ID, request, false); !errors.Is(err, library.ErrMediaOperationConflict) {
		t.Fatalf("inactive inventory accepted a mismatched apply receipt: %v", err)
	}
}
