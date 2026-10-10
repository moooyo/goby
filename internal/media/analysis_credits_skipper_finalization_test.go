package media

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestCreditsSkipperEmptyOutcomeDoesNotSurviveProofOrRetirementFailure(t *testing.T) {
	closeFailure := errors.New("tool descriptor close failed")
	sourceFailure := errors.New("final source identity changed")
	extractionFailure := errors.New("audio process failed")
	for _, test := range []struct {
		name      string
		initial   error
		closeErr  error
		sourceErr error
		expired   bool
	}{
		{name: "completed-empty", initial: ErrIntroSkipperFingerprintUnavailable},
		{name: "empty-close", initial: ErrIntroSkipperFingerprintUnavailable, closeErr: closeFailure},
		{name: "empty-source", initial: ErrIntroSkipperFingerprintUnavailable, sourceErr: sourceFailure},
		{name: "empty-deadline", initial: ErrIntroSkipperFingerprintUnavailable, expired: true},
		{name: "empty-close-source-deadline", initial: ErrIntroSkipperFingerprintUnavailable, closeErr: closeFailure, sourceErr: sourceFailure, expired: true},
		{name: "process-close-source", initial: extractionFailure, closeErr: closeFailure, sourceErr: sourceFailure},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			if test.expired {
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Unix(0, 0))
				defer cancel()
			}
			result := CreditsSkipperFeatures{RawFingerprint: []uint32{1}, SourceIdentity: "source", AlgorithmProfile: "profile"}
			resultErr := test.initial
			// The actual deferred tool close feeds the same failure transition
			// before final source/context checks and slot release.
			failCreditsSkipperAnalysis(test.closeErr, &result, &resultErr)
			released := false
			finalizeCreditsSkipperAnalysis(ctx, func() error {
				if released {
					t.Fatal("source proof ran after slot release")
				}
				return test.sourceErr
			}, func() { released = true }, &result, &resultErr)
			failed := test.closeErr != nil || test.sourceErr != nil || test.expired
			wantEmpty := test.initial == ErrIntroSkipperFingerprintUnavailable && !failed
			if !released || errors.Is(resultErr, ErrIntroSkipperFingerprintUnavailable) != wantEmpty {
				t.Fatalf("completed-empty classification survived a real failure: %v", resultErr)
			}
			if failed && !reflect.DeepEqual(result, CreditsSkipperFeatures{}) {
				t.Fatalf("failed retirement or proof retained metadata: %+v", result)
			}
			for _, want := range []error{test.closeErr, test.sourceErr} {
				if want != nil && !errors.Is(resultErr, want) {
					t.Fatalf("lost an independent failure %v: %v", want, resultErr)
				}
			}
			if test.expired && !errors.Is(resultErr, context.DeadlineExceeded) {
				t.Fatalf("bounded deadline disappeared: %v", resultErr)
			}
			if test.initial == extractionFailure && !errors.Is(resultErr, extractionFailure) {
				t.Fatalf("tool/source retirement hid the extraction failure: %v", resultErr)
			}
		})
	}
}
