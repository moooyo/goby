package library

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/moooyo/goby/internal/media"
)

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
