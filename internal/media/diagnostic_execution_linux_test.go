package media

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

type diagnosticFakeOwnedStages struct {
	*diagnosticFakeStageSession
	closes int
}

func (session *diagnosticFakeOwnedStages) close() error {
	session.closes++
	session.interrupt()
	return nil
}

func diagnosticLinuxTestExecution(t *testing.T) (*DiagnosticExecution, *diagnosticFakeOwnedStages) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	session := &diagnosticFakeOwnedStages{diagnosticFakeStageSession: &diagnosticFakeStageSession{t: t, ctx: ctx, cancel: cancel}}
	owner, err := newDiagnosticExecution(ctx, DiagnosticExecutionOptions{}, func(context.Context, diagnosticProcessOptions) (diagnosticExecutionBackend, error) {
		return &diagnosticLinuxExecution{session: session}, nil
	})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { owner.Close(); cancel() })
	return owner, session
}

func TestDiagnosticExecutionAuthorityIsRequiredBeforeVersionWithoutPrivateError(t *testing.T) {
	private := errors.New("private-user-and-store-detail")
	for _, authorize := range []func(context.Context) error{nil, func(context.Context) error { return private }} {
		owner, session := diagnosticLinuxTestExecution(t)
		report, err := owner.Run(DiagnosticSelection{}, authorize, nil)
		if err != ErrDiagnosticAuthority || errors.Is(err, private) || session.calls != 0 || report.CommandsStarted != 0 || report.State != "cancelled" || report.Code != "diagnostic_authority_lost" || !report.SessionClosureRequired {
			t.Fatal("missing authority reached a command or lost its safe rejection")
		}
		data, marshalErr := json.Marshal(report)
		if marshalErr != nil || strings.Contains(string(data), private.Error()) {
			t.Fatal("authority rejection leaked private error text")
		}
		if session.contextErr() == nil || session.closes != 0 {
			t.Fatal("authority rejection failed to cancel or falsely finalized closure")
		}
	}
}

func TestDiagnosticExecutionAuthorityIsRecheckedForEveryVersionAndMediaCommand(t *testing.T) {
	owner, session := diagnosticLinuxTestExecution(t)
	checks := 0
	report, err := owner.Run(DiagnosticSelection{}, func(ctx context.Context) error {
		if session.calls != checks {
			t.Fatal("authorization was skipped, cached, or repeated after dispatch")
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > diagnosticRunDeadline {
			t.Fatal("authorization callback escaped the total stage budget")
		}
		checks++
		return nil
	}, nil)
	if err != nil || report.State != "stages_complete" || checks != 17 || session.calls != 17 || !report.SessionClosureRequired {
		t.Fatal("one authority check did not precede every command")
	}
	if err = owner.Close(); err != nil || session.closes != 1 {
		t.Fatal("completed stages did not retain explicit session closure")
	}
}

func TestDiagnosticExecutionAuthorityLossPreservesCompletedStagesAndStopsDispatch(t *testing.T) {
	owner, session := diagnosticLinuxTestExecution(t)
	checks := 0
	report, err := owner.Run(DiagnosticSelection{}, func(context.Context) error {
		checks++
		if session.calls == 8 {
			return errors.New("administrator no longer enabled")
		}
		return nil
	}, nil)
	if err != ErrDiagnosticAuthority || report.State != "cancelled" || report.Code != "diagnostic_authority_lost" || checks != 9 || session.calls != 8 || report.CommandsStarted != 8 {
		t.Fatal("authority loss was retried or allowed subsequent media commands")
	}
	if report.Stages[0].State != "passed" || report.Stages[1].State != "cancelled" {
		t.Fatal("authority loss erased completed evidence or misreported the rejected stage")
	}
	for _, stage := range report.Stages[2:] {
		if stage.State != "not_run" || stage.Code != "diagnostic_authority_lost" {
			t.Fatal("unattempted stages lost the authority boundary")
		}
	}
}

func TestDiagnosticExecutionCancelDuringAuthorityCallbackNeverStartsCommand(t *testing.T) {
	owner, session := diagnosticLinuxTestExecution(t)
	entered, done := make(chan struct{}), make(chan struct{})
	var report DiagnosticReport
	var runErr error
	go func() {
		defer close(done)
		report, runErr = owner.Run(DiagnosticSelection{}, func(ctx context.Context) error {
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		}, nil)
	}()
	diagnosticExecutionWait(t, entered)
	owner.Cancel()
	diagnosticExecutionWait(t, done)
	if !errors.Is(runErr, context.Canceled) || report.Code != "diagnostic_cancelled" || session.calls != 0 || !report.SessionClosureRequired {
		t.Fatal("cancelled authority callback started work or claimed credential rejection")
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDiagnosticExecutionSuccessfulAuthorityCallbackCannotOverrideCancellation(t *testing.T) {
	owner, session := diagnosticLinuxTestExecution(t)
	report, err := owner.Run(DiagnosticSelection{}, func(context.Context) error {
		owner.Cancel()
		return nil
	}, nil)
	if !errors.Is(err, context.Canceled) || session.calls != 0 || report.State != "cancelled" {
		t.Fatal("successful callback overrode a concurrent owner cancellation")
	}
}
