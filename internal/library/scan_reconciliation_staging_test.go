package library

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestScanStagingLookupBoundsApplyBeforeDatabaseAccess(t *testing.T) {
	var stage *scanReconciliationStaging
	tooMany := make([]string, scanReconciliationStagingBatchIDs+1)
	tooManyBytes := make([]string, scanReconciliationStagingBatchIDs)
	for index := range tooManyBytes {
		// Each valid ID is 252 UTF-8 bytes, not 126 bytes. The binary text-array
		// header puts this 512-element request beyond the 128 KiB wire budget.
		tooManyBytes[index] = strings.Repeat("\u00e9", 126)
	}
	for _, test := range []struct {
		name string
		ids  []string
		want error
	}{
		{"item_count", tooMany, errScanReconciliationStagingBudget},
		{"serialized_utf8_bytes", tooManyBytes, errScanReconciliationStagingBudget},
		{"oversized_identity", []string{strings.Repeat("x", 257)}, ErrInvalidInput},
		{"invalid_utf8", []string{"\xff"}, ErrInvalidInput},
		{"control_character", []string{"item\x00id"}, ErrInvalidInput},
		{"empty_identity", []string{""}, ErrInvalidInput},
		{"empty_request_is_not_an_empty_sealed_pass", nil, errScanReconciliationStagingState},
	} {
		t.Run(test.name, func(t *testing.T) {
			found, err := stage.Contains(nil, test.ids)
			if found != nil || !errors.Is(err, test.want) {
				t.Fatalf("invalid or unbound lookup returned membership: %#v, %v", found, err)
			}
		})
	}
}

func TestScanStagingCancellationCannotBeSealedByAFreshContext(t *testing.T) {
	owner := &scanOwnership{}
	store := &Store{ownership: owner}
	stage := &scanReconciliationStaging{store: store, owner: owner}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := stage.Record(ctx, "accepted-item"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled record did not retain its failure: %v", err)
	}
	if err := stage.Seal(context.Background()); !errors.Is(err, context.Canceled) || stage.sealed.Load() {
		t.Fatalf("a fresh context cleared incomplete staging: %v", err)
	}
	if found, err := stage.Contains(nil, nil); found != nil || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("cancelled staging became an empty complete set: %#v, %v", found, err)
	}
}

func TestScanStagingFailuresAreNotFilesystemObservationFailures(t *testing.T) {
	statement := &pgconn.PgError{Code: "23514", Message: "test staging constraint"}
	for _, err := range []error{errScanReconciliationStagingBudget, errScanReconciliationStagingState,
		statement, errors.Join(errScanReconciliationStagingBudget, statement), context.Canceled} {
		if scanReconciliationObservationOnly(err) {
			t.Fatalf("staging failure was silently downgraded to a retained-records observation: %v", err)
		}
	}
	if !errors.Is(errScanReconciliationStagingState, ErrUnavailable) {
		t.Fatal("missing session state did not fence deletion authority")
	}
}
