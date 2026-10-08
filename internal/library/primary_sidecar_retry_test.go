package library

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/primaryio"
)

func TestSidecarAdmissionRefreshRetainsOwnerWithoutIOLease(t *testing.T) {
	refreshFailure := errors.New("fresh authority failed")
	for _, scenario := range []struct {
		name string
		want error
	}{
		{"success", nil},
		{"refresh_failure", refreshFailure},
		{"caller_cancellation", context.Canceled},
		{"owner_cancellation", context.Canceled},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			operation, governor, owners, finished := primaryRootIOTestFixture(t, 1)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			refreshed := false
			err := waitSidecarCapacityAndRefresh(ctx, operation, "root-0", primaryio.Background, func(work context.Context) error {
				refreshed = true
				if err := work.Err(); err != nil {
					return fmt.Errorf("fresh context was already canceled: %w", err)
				}
				if stats := governor.Stats(); stats.Active != 0 || stats.Queued != 0 {
					return fmt.Errorf("SQL refresh retained actual I/O admission: %+v", stats)
				}
				if owners.Stats().RegisteredOwners != 1 {
					return errors.New("SQL refresh lost its retained owner")
				}
				switch scenario.name {
				case "refresh_failure":
					return refreshFailure
				case "caller_cancellation":
					cancel()
				case "owner_cancellation":
					if err := operation.handle.state.owner.Cancel(); err != nil {
						return err
					}
				default:
					return nil
				}
				select {
				case <-work.Done():
					return work.Err()
				case <-time.After(5 * time.Second):
					return errors.New("SQL refresh did not observe cancellation")
				}
			})
			if !refreshed || !errors.Is(err, scenario.want) {
				t.Fatalf("refresh result=%v want=%v refreshed=%v", err, scenario.want, refreshed)
			}
			if owners.Stats().RegisteredOwners != 0 || governor.Stats().Active != 0 {
				t.Fatalf("refresh did not retire admission: owners=%+v IO=%+v", owners.Stats(), governor.Stats())
			}
			primaryRootIOTestWait(t, finished, "sidecar refresh owner completion")
		})
	}
}

func TestSidecarAdmissionRetriesWrappedCapacityWithoutRetryingUnknownRetirement(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want bool
	}{
		{"capacity", ErrBusy, true},
		{"legacy unavailable API category", fmt.Errorf("%w: publication capacity: %w", ErrUnavailable, ErrBusy), true},
		{"descriptor uncertainty", errors.Join(ErrBusy, errSidecarRetirementUnknown), false},
		{"rollback uncertainty", errors.Join(ErrBusy, errSidecarRollbackUnknown), false},
		{"process uncertainty", errors.Join(ErrBusy, media.ErrProcessRetirementUnknown), false},
		{"mixed filesystem failure", errors.Join(ErrBusy, io.ErrUnexpectedEOF), false},
		{"mixed observation failure", errors.Join(ErrBusy, errStorageObservationUnavailable), false},
		{"changed source", errors.Join(ErrBusy, ErrSourceChanged), false},
		{"canceled", errors.Join(ErrBusy, context.Canceled), false},
		{"deadline", errors.Join(ErrBusy, context.DeadlineExceeded), false},
		{"other unavailable", ErrUnavailable, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := sidecarAdmissionRetryable(test.err); got != test.want {
				t.Fatalf("capacity retry classification=%v want=%v error=%v", got, test.want, test.err)
			}
		})
	}
}

func TestSidecarSourceChangeWarningDoesNotHideUnknownRetirement(t *testing.T) {
	state := &scanState{warnings: 3}
	err := state.retrySidecarScan(func() error {
		return fmt.Errorf("%w: final sidecar proof: %w", ErrUnavailable, ErrSourceChanged)
	})
	if err != nil || state.warnings != 4 {
		t.Fatalf("ordinary changed source did not retain the warning: warnings=%d error=%v", state.warnings, err)
	}
	unknown := errors.Join(ErrSourceChanged, errSidecarRetirementUnknown)
	if err := state.retrySidecarScan(func() error { return unknown }); !errors.Is(err, errSidecarRetirementUnknown) || state.warnings != 4 {
		t.Fatalf("descriptor uncertainty became a source warning: warnings=%d error=%v", state.warnings, err)
	}
	processUnknown := errors.Join(ErrSourceChanged, media.ErrProcessRetirementUnknown)
	if err := state.retrySidecarScan(func() error { return processUnknown }); !errors.Is(err, media.ErrProcessRetirementUnknown) || state.warnings != 4 {
		t.Fatalf("process uncertainty became a source warning: warnings=%d error=%v", state.warnings, err)
	}
	rollbackUnknown := errors.Join(ErrSourceChanged, errSidecarRollbackUnknown)
	if err := state.retrySidecarScan(func() error { return rollbackUnknown }); !errors.Is(err, errSidecarRollbackUnknown) || state.warnings != 4 {
		t.Fatalf("transaction uncertainty became a source warning: warnings=%d error=%v", state.warnings, err)
	}
}
