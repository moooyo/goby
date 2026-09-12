package library

import (
	"context"
	"errors"
	"os"
	"testing"
)

type rootBindingScanTestCapture struct {
	snapshot     RootTopologySnapshot
	snapshotErr  error
	snapshotHook func()
	cloneErr     error
	cloneHook    func()
	revalidate   func(context.Context, int) error
	anchor       *os.Root
	checks       int
	closes       int
}

func (capture *rootBindingScanTestCapture) Snapshot() (RootTopologySnapshot, error) {
	if capture.snapshotHook != nil {
		capture.snapshotHook()
	}
	return capture.snapshot.Clone(), capture.snapshotErr
}

func (capture *rootBindingScanTestCapture) CloneApprovedAnchor() (*os.Root, error) {
	if capture.cloneHook != nil {
		capture.cloneHook()
	}
	if capture.cloneErr != nil {
		return nil, capture.cloneErr
	}
	var err error
	capture.anchor, err = os.OpenRoot(capture.snapshot.Mapping.ApprovedPath)
	return capture.anchor, err
}

func (capture *rootBindingScanTestCapture) CloneRegisteredRoot() (*os.Root, error) {
	return os.OpenRoot(capture.snapshot.Mapping.RegisteredPath)
}

func (capture *rootBindingScanTestCapture) Revalidate(ctx context.Context) error {
	capture.checks++
	if capture.revalidate != nil {
		return capture.revalidate(ctx, capture.checks)
	}
	return ctx.Err()
}

func (capture *rootBindingScanTestCapture) Close() error {
	capture.closes++
	return nil
}

func TestRootBindingScanObservationClassificationKeepsTransactionFailuresFatal(t *testing.T) {
	observation := rootBindingScanObservationFailure{err: ErrRootTopologyUnavailable}
	for _, test := range []struct {
		name string
		err  error
		soft bool
	}{
		{"direct observation", observation, true},
		{"joined observation", errors.Join(errors.Join(observation)), true},
		{"cancelled observation", rootBindingScanObservationFailure{err: context.Canceled}, true},
		{"ordinary database failure", errors.New("database failure"), false},
		{"ownership unavailable", ErrUnavailable, false},
		{"rollback failure", errors.Join(observation, errors.New("rollback failed")), false},
		{"ownership rollback failure", errors.Join(observation, ErrUnavailable), false},
		{"nested rollback failure", errors.Join(errors.Join(observation), errors.Join(ErrUnavailable)), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, soft := rootBindingScanObservationOnly(test.err)
			if soft != test.soft || soft && got == nil {
				t.Fatalf("observation classification = %v, %v; want soft = %v", got, soft, test.soft)
			}
		})
	}
}

func TestRootBindingScanCaptureClosesIndependentRootAndRejectsUnverifiedEvidence(t *testing.T) {
	opened, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	retained, err := opened.OpenRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	defer retained.Close()
	inner := &rootBindingScanTestCapture{}
	capture := &rootBindingScanCapture{status: RootBindingVerified, opened: opened, capture: inner}
	if err := capture.Revalidate(context.Background()); err != nil || inner.checks != 1 {
		t.Fatalf("verified evidence failed validation: %v", err)
	}
	if err := capture.Close(); err != nil {
		t.Fatal(err)
	}
	if err := capture.Close(); err != nil || inner.closes != 1 {
		t.Fatalf("capture close was not idempotent: %v, closes = %d", err, inner.closes)
	}
	if _, err := opened.Stat("."); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("scan evidence retained its owned root: %v", err)
	}
	if _, err := retained.Stat("."); err != nil {
		t.Fatalf("capture close invalidated an independent lease: %v", err)
	}
	if err := capture.Revalidate(context.Background()); !errors.Is(err, ErrRootTopologyUnavailable) {
		t.Fatalf("closed evidence revalidated: %v", err)
	}
	for _, status := range []RootBindingStatus{RootBindingUnbound, RootBindingMismatch, RootBindingUnavailable} {
		unverified := &rootBindingScanCapture{status: status, opened: retained, capture: inner}
		if err := unverified.Revalidate(context.Background()); !errors.Is(err, ErrRootTopologyUnavailable) {
			t.Fatalf("%s evidence revalidated: %v", status, err)
		}
	}
	var absent *rootBindingScanCapture
	if err := absent.Revalidate(context.Background()); !errors.Is(err, ErrRootTopologyUnavailable) {
		t.Fatalf("absent evidence revalidated: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := absent.Revalidate(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled evidence did not retain cancellation: %v", err)
	}
}
