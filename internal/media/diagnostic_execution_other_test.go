//go:build !linux

package media

import (
	"context"
	"errors"
	"testing"
)

func TestDiagnosticExecutionUnavailablePlatformHasNoFallbackOwner(t *testing.T) {
	owner, err := NewDiagnosticExecution(context.Background(), DiagnosticExecutionOptions{FFmpegPath: "must-not-run"})
	if owner != nil || !errors.Is(err, ErrDiagnosticResources) {
		t.Fatal("unsupported platform provided an unsandboxed execution owner")
	}
	owner.Cancel()
	if err = owner.Close(); err != nil {
		t.Fatal("nil unavailable owner acquired cleanup resources")
	}
	report, err := owner.Run(DiagnosticSelection{}, nil, nil)
	if !errors.Is(err, ErrDiagnosticResources) || report.State != "unavailable" || !report.SessionClosureRequired {
		t.Fatal("unavailable platform produced executable or finalized evidence")
	}
}
