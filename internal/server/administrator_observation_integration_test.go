//go:build linux

package server

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/diagnostics"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

type administratorObservationTrace struct {
	queries atomic.Int64
	close   bool
}

func (trace *administratorObservationTrace) TraceQueryStart(ctx context.Context, connection *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	trace.queries.Add(1)
	if trace.close {
		_ = connection.Close(ctx)
	}
	return ctx
}

func (*administratorObservationTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func TestAdministratorObservationsUseOneStatementAndPreserveErrors(t *testing.T) {
	f, cookie, _, _ := adminSettingsHTTPFixture(t)
	actor, err := f.users.Resolve(f.ctx, cookie.Value, "admin")
	if err != nil {
		t.Fatal(err)
	}
	trace := &administratorObservationTrace{}
	configuration := f.pool.Config()
	configuration.ConnConfig.Tracer = trace
	reader, err := pgxpool.NewWithConfig(f.ctx, configuration)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.Close)
	app := &Server{db: reader}
	operations := &mediaOperationsRuntime{server: app}
	for _, test := range []struct {
		name        string
		read        func(context.Context) error
		cancelError error
		lostError   error
	}{
		{"analysis", func(ctx context.Context) error { return app.checkAdminMediaAnalysisActor(ctx, actor) }, library.ErrUnavailable, library.ErrUnavailable},
		{"observability", func(ctx context.Context) error {
			return app.checkObservabilityAdministrator(ctx, actor, identity.AdministratorNative)
		}, diagnostics.ErrUnavailable, diagnostics.ErrUnavailable},
		{"diagnostic", func(ctx context.Context) error { return app.checkMediaDiagnosticActor(ctx, actor) }, context.Canceled, identity.ErrAdministratorReadUnavailable},
		{"operations", func(ctx context.Context) error { return operations.authorize(ctx, actor) }, context.Canceled, identity.ErrAdministratorReadUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			trace.queries.Store(0)
			if err := test.read(f.ctx); err != nil {
				t.Fatal(err)
			}
			if count := trace.queries.Load(); count != 1 {
				t.Fatalf("runtime authorization issued %d statements, want 1", count)
			}
			cancelled, cancel := context.WithCancel(f.ctx)
			cancel()
			if err := test.read(cancelled); !errors.Is(err, test.cancelError) {
				t.Fatalf("cancelled connection admission returned %v, want %v", err, test.cancelError)
			}
			trace.close = true
			err := test.read(f.ctx)
			trace.close = false
			if !errors.Is(err, test.lostError) || errors.Is(err, identity.ErrUnauthorized) {
				t.Fatalf("connection loss returned %v, want %v", err, test.lostError)
			}
			if _, err := f.pool.Exec(f.ctx, "UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1", actor.SessionID); err != nil {
				t.Fatal(err)
			}
			err = test.read(f.ctx)
			if _, restoreErr := f.pool.Exec(f.ctx, "UPDATE sessions SET revoked_at=NULL WHERE id=$1", actor.SessionID); restoreErr != nil {
				t.Fatal(restoreErr)
			}
			if !errors.Is(err, identity.ErrUnauthorized) {
				t.Fatalf("runtime observation retained a revoked credential: %v", err)
			}
		})
	}
}
