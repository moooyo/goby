package media

import (
	"context"
	"os"
)

type diagnosticOwnedStageSession interface {
	diagnosticStageSession
	close() error
}

type diagnosticLinuxExecution struct {
	session diagnosticOwnedStageSession
}

func openDiagnosticExecution(ctx context.Context, options diagnosticProcessOptions) (diagnosticExecutionBackend, error) {
	session, err := newDiagnosticProcessSession(ctx, options)
	if session == nil {
		return nil, err
	}
	return &diagnosticLinuxExecution{session: session}, err
}

func (execution *diagnosticLinuxExecution) execute(ctx context.Context, selection DiagnosticSelection, authorize func(context.Context) error, progress func(DiagnosticReport)) (DiagnosticReport, error) {
	// Include authority callbacks in the same total stage deadline. The graph's
	// child context cannot extend this budget or the original process session.
	budget, cancel := context.WithTimeout(ctx, diagnosticRunDeadline)
	defer cancel()
	session := &diagnosticAuthorizedSession{ctx: budget, session: execution.session, authorize: authorize}
	return executeDiagnosticStages(budget, session, selection, progress)
}

func (execution *diagnosticLinuxExecution) interrupt()   { execution.session.interrupt() }
func (execution *diagnosticLinuxExecution) close() error { return execution.session.close() }

type diagnosticAuthorizedSession struct {
	ctx       context.Context
	session   diagnosticStageSession
	authorize func(context.Context) error
	denied    bool
}

func (session *diagnosticAuthorizedSession) contextErr() error {
	if session.denied {
		return ErrDiagnosticAuthority
	}
	if err := session.ctx.Err(); err != nil {
		return err
	}
	return session.session.contextErr()
}

func (session *diagnosticAuthorizedSession) interrupt() { session.session.interrupt() }

func (session *diagnosticAuthorizedSession) checkAuthority() error {
	if err := session.contextErr(); err != nil {
		return err
	}
	if session.authorize == nil {
		session.denied = true
		session.session.interrupt()
		return ErrDiagnosticAuthority
	}
	if err := session.authorize(session.ctx); err != nil {
		if cancelled := session.contextErr(); cancelled != nil {
			return cancelled
		}
		session.denied = true
		session.session.interrupt()
		return ErrDiagnosticAuthority
	}
	// Cancellation during a successful callback still prevents dispatch. The
	// middleware's prior principal is never substituted for this live callback.
	return session.contextErr()
}

func (session *diagnosticAuthorizedSession) version() (diagnosticCommandObservation, error) {
	if err := session.checkAuthority(); err != nil {
		return diagnosticCommandObservation{}, err
	}
	return session.session.version()
}

func (session *diagnosticAuthorizedSession) run(plan DiagnosticPlan, input *os.File, hash string) (diagnosticCommandObservation, error) {
	if err := session.checkAuthority(); err != nil {
		return diagnosticCommandObservation{}, err
	}
	return session.session.run(plan, input, hash)
}
