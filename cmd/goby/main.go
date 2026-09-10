package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/diagnostics"
	"github.com/moooyo/goby/internal/lifecycle"
	"github.com/moooyo/goby/internal/recovery"
)

var version = "0.1.0-dev"

func main() {
	fallback := slog.NewJSONHandler(os.Stdout, nil)
	defer func() {
		if recover() != nil {
			// Never allow an unexpected process panic to print its payload or stack.
			slog.New(diagnostics.NewHandler(nil, fallback)).Error("server stopped")
			os.Exit(1)
		}
	}()
	if err := run(fallback); err != nil {
		os.Exit(1)
	}
}

func run(fallback slog.Handler) (runErr error) {
	bootstrapLogger := slog.New(diagnostics.NewHandler(nil, fallback))
	logger := bootstrapLogger
	slog.SetDefault(logger)
	var diagnosticStore *diagnostics.Store
	var shutdownStarted time.Time
	var cliHandled bool
	defer func() {
		if recover() != nil {
			runErr = errors.New("server stopped unexpectedly")
		}
		// Generation workers, pools and leases drain before the shared runtime,
		// and both finish before this final diagnostic event and store close.
		if !cliHandled {
			if runErr != nil {
				logger.Error("server stopped", "error", runErr)
			} else if !shutdownStarted.IsZero() {
				logger.Info("server shutdown completed", "duration_ms", time.Since(shutdownStarted).Milliseconds())
			} else {
				logger.Info("server stopped")
			}
		}
		slog.SetDefault(bootstrapLogger)
		if diagnosticStore != nil {
			if err := diagnosticStore.Close(); err != nil {
				bootstrapLogger.Error("background work did not close cleanly", "error", err)
				runErr = errors.Join(runErr, err)
			}
		}
	}()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cliHandled, err = runCLI(ctx, cfg, os.Args[1:])
	if cliHandled || err != nil {
		return err
	}
	diagnosticStore, err = diagnostics.Open(cfg.Diagnostics)
	if err != nil {
		return err
	}
	logger = slog.New(diagnostics.NewHandler(diagnosticStore, fallback))
	slog.SetDefault(logger)
	logger.Info("server starting", "version", version, "database", "postgresql")
	startup, cancelStartup := context.WithTimeout(ctx, cfg.StartupTimeout)
	runtime, err := recovery.Open(startup, cfg)
	cancelStartup()
	if err != nil {
		return generationError("recovery runtime initialization failed", err)
	}
	owned := &generationOwner{runtime: runtime, generations: make(map[*generation]bool)}
	defer func() { runErr = errors.Join(runErr, owned.Close()) }()
	if err := recoverRuntimeStartup(ctx, cfg, runtime); err != nil {
		return err
	}
	if err := runGenerationLoop(ctx, cfg, owned, logger, diagnosticStore); err != nil {
		if ctx.Err() == nil {
			return err
		}
		// A signal cancels this process, while a lost database lease cancels
		// only its generation. Pending recovery remains durable for startup.
		shutdownStarted = time.Now()
		return nil
	}
	shutdownStarted = time.Now()
	return nil
}

// The owner also tracks partial startup results and unconsumed switch resources.
// A timed-out public Close does not permit closing shared runtime stores; the
// final join below keeps those dependencies alive until every worker is done.
type generationOwner struct {
	runtime     *recovery.Runtime
	generations map[*generation]bool
}

func (o *generationOwner) add(g *generation) {
	if g != nil {
		o.generations[g] = true
	}
}

func (o *generationOwner) close(g *generation, allowForcedHTTP bool) error {
	if g == nil {
		return nil
	}
	bounded, cancel := context.WithTimeout(context.Background(), generationShutdownTimeout)
	_ = g.Close(bounded)
	cancel()
	err := g.Close(context.Background())
	delete(o.generations, g)
	if allowForcedHTTP && g.stopResourceErr == nil &&
		(g.stopGraceErr == nil || errors.Is(g.stopGraceErr, context.DeadlineExceeded) || errors.Is(g.stopGraceErr, context.Canceled)) {
		return g.resourceCloseErr
	}
	return err
}

func (o *generationOwner) Close() error {
	for g := range o.generations {
		g.cancel()
	}
	var result error
	for g := range o.generations {
		result = errors.Join(result, o.close(g, false))
	}
	if o.runtime != nil {
		result = errors.Join(result, generationCall("recovery runtime cleanup failed", o.runtime.Close))
	}
	return result
}

func (o *generationOwner) claimCandidate(ctx context.Context, candidate *recovery.SwitchCandidate) *generation {
	if candidate == nil {
		return nil
	}
	lifetime, cancel := context.WithCancel(ctx)
	resource := &generation{ctx: lifetime, cancel: cancel, pool: candidate.Pool, lease: candidate.Lease, state: candidate.State}
	candidate.Pool, candidate.Lease = nil, nil
	o.add(resource)
	return resource
}

func (o *generationOwner) prepare(ctx context.Context, logger *slog.Logger, diag *diagnostics.Store, input *generation) (*generation, error) {
	if input == nil {
		g, err := prepareGeneration(ctx, o.runtime, logger, diag, version, nil, nil)
		o.add(g)
		return g, err
	}
	pool, lease, expected := input.pool, input.lease, input.state
	// Transfer once, before calling the builder, whose failure path owns both
	// resources. The carrier has never started workers or exposed a listener.
	input.pool, input.lease = nil, nil
	input.cancel()
	delete(o.generations, input)
	g, err := prepareGeneration(ctx, o.runtime, logger, diag, version, pool, lease)
	o.add(g)
	if err == nil && g.state != expected {
		err = errors.New("prepared generation differs from its activated candidate")
	}
	return g, err
}

func recoverRuntimeStartup(ctx context.Context, cfg config.Config, runtime *recovery.Runtime) error {
	startup, cancel := context.WithTimeout(ctx, cfg.StartupTimeout)
	defer cancel()
	_, _, err := runtime.RecoverStartupTransition(startup)
	return generationError("durable recovery startup transition could not be resolved", err)
}

type generationRecoveryExpectation struct {
	state               *lifecycle.State
	allowReturn         bool
	acceptanceUncertain bool
}

// Failed preparation may ask the authoritative coordinator to return an
// unaccepted generation. Once AcceptSwitch has been attempted, all uncertainty
// is resolved forward against the already activated state, never by rollback.
func recoverGenerationFailure(ctx context.Context, cfg config.Config, runtime *recovery.Runtime, cause error,
	expect generationRecoveryExpectation, resume *lifecycle.State) (generationRecoveryExpectation, error) {
	if ctx.Err() != nil {
		return expect, ctx.Err()
	}
	work, cancel := context.WithTimeout(ctx, cfg.Recovery.WithDefaults().OperationTimeout)
	defer cancel()
	id, activated, err := runtime.RecoverStartupTransition(work)
	if err != nil {
		return expect, errors.Join(cause, generationError("recovery transition inspection failed", err))
	}
	_, state, err := runtime.ActiveConfig(work)
	if err != nil {
		return expect, errors.Join(cause, generationError("active recovery generation is unavailable", err))
	}
	if expect.state != nil && state != *expect.state {
		return expect, errors.Join(cause, errors.New("recovery changed the expected activated generation"))
	}
	if expect.acceptanceUncertain {
		if id != "" && !activated {
			return expect, errors.Join(cause, errors.New("uncertain acceptance did not retain its activated generation"))
		}
		return expect, nil
	}
	if !expect.allowReturn {
		if expect.state != nil {
			return expect, nil
		}
		return expect, cause
	}
	if id == "" {
		if resume != nil && state == *resume {
			return generationRecoveryExpectation{allowReturn: true}, nil
		}
		return expect, cause
	}
	if err := runtime.ReturnUnaccepted(work); err == nil {
		return generationRecoveryExpectation{allowReturn: true}, nil
	}
	// A refused or uncertain return is not permission to select old config.
	// Reread durable state and retry only that exact generation.
	if _, _, err := runtime.RecoverStartupTransition(work); err != nil {
		return expect, errors.Join(cause, generationError("recovery return outcome could not be resolved", err))
	}
	_, state, err = runtime.ActiveConfig(work)
	if err != nil {
		return expect, errors.Join(cause, generationError("recovery return generation is unavailable", err))
	}
	return generationRecoveryExpectation{state: &state}, nil
}

func drainGenerationForSwitch(ctx context.Context, g *generation) error {
	bounded, cancel := context.WithTimeout(ctx, generationShutdownTimeout)
	_ = g.StopServing(bounded)
	cancel()
	_ = g.StopServing(context.Background())
	if g.stopResourceErr != nil {
		return g.stopResourceErr
	}
	if g.stopGraceErr != nil && !errors.Is(g.stopGraceErr, context.DeadlineExceeded) && !errors.Is(g.stopGraceErr, context.Canceled) {
		return g.stopGraceErr
	}
	bounded, cancel = context.WithTimeout(ctx, generationShutdownTimeout)
	_ = g.CloseApplication(bounded)
	cancel()
	if err := g.CloseApplication(context.Background()); err != nil {
		return err
	}
	return ctx.Err()
}

func prepareGenerationSwitch(ctx context.Context, cfg config.Config, owned *generationOwner, current *generation, id string) (*generation, error) {
	if err := drainGenerationForSwitch(ctx, current); err != nil {
		return nil, errors.Join(err, owned.close(current, true))
	}
	work, cancel := context.WithTimeout(ctx, cfg.Recovery.WithDefaults().OperationTimeout)
	candidate, err := current.manager.PrepareSwitch(work, id)
	cancel()
	input := owned.claimCandidate(ctx, candidate)
	if err == nil && (candidate == nil || input.pool == nil || input.lease == nil || candidate.OperationID != id) {
		err = errors.New("recovery returned an incomplete switch candidate")
	}
	closeErr := owned.close(current, true)
	if err != nil || closeErr != nil {
		return nil, errors.Join(generationError("recovery generation switch preparation failed", err), closeErr, owned.close(input, true))
	}
	return input, nil
}

func acceptPreparedGeneration(ctx context.Context, cfg config.Config, g *generation) error {
	if g.pendingSwitch == "" {
		return nil
	}
	if !g.switchActivated || g.app == nil || g.listener == nil {
		return errors.New("recovery generation is not prepared for acceptance")
	}
	if err := g.checkStartup(ctx); err != nil {
		return err
	}
	work, cancel := context.WithTimeout(ctx, cfg.Recovery.WithDefaults().OperationTimeout)
	defer cancel()
	if err := g.manager.AcceptSwitch(work); err != nil {
		return generationError("recovery generation acceptance could not be confirmed", err)
	}
	g.mu.Lock()
	g.pendingSwitch, g.switchActivated = "", false
	g.mu.Unlock()
	return nil
}

func runGenerationLoop(ctx context.Context, cfg config.Config, owned *generationOwner, logger *slog.Logger, diag *diagnostics.Store) error {
	var input *generation
	expect := generationRecoveryExpectation{allowReturn: true}
	failures := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		g, err := owned.prepare(ctx, logger, diag, input)
		input = nil
		if err == nil && expect.state != nil && g.state != *expect.state {
			err = errors.New("startup selected a different recovered generation")
		}
		if err != nil {
			cause := errors.Join(err, owned.close(g, true))
			failures++
			if failures >= 3 {
				return errors.Join(cause, errors.New("recovery startup retry budget was exhausted"))
			}
			expect, err = recoverGenerationFailure(ctx, cfg, owned.runtime, cause, expect, nil)
			if err != nil {
				return err
			}
			continue
		}
		var switchID string
		if g.pendingSwitch != "" && !g.switchActivated {
			if expect.acceptanceUncertain {
				return errors.New("uncertain acceptance cannot retire another source generation")
			}
			switchID = g.pendingSwitch
		} else {
			if err := acceptPreparedGeneration(ctx, cfg, g); err != nil {
				state := g.state
				cause := errors.Join(err, owned.close(g, true))
				failures++
				if failures >= 3 {
					return errors.Join(cause, errors.New("recovery acceptance retry budget was exhausted"))
				}
				expect = generationRecoveryExpectation{state: &state, acceptanceUncertain: true}
				expect, err = recoverGenerationFailure(ctx, cfg, owned.runtime, cause, expect, nil)
				if err != nil {
					return err
				}
				continue
			}
			failure := g.Start()
			logger.Info("server listening", "version", version, "database", "postgresql")
			failures, expect = 0, generationRecoveryExpectation{allowReturn: true}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-g.ctx.Done():
				if !g.lease.Protects(g.pool) {
					return database.ErrLeaseUnavailable
				}
				return errors.New("active application generation stopped")
			case err := <-failure:
				if err == nil || errors.Is(err, http.ErrServerClosed) {
					return errors.New("active HTTP generation stopped unexpectedly")
				}
				return err
			case id, open := <-g.manager.SwitchRequests():
				if !open || id == "" {
					return errors.New("recovery switch notification is unavailable")
				}
				switchID = id
			}
		}
		previous := g.state
		input, err = prepareGenerationSwitch(ctx, cfg, owned, g, switchID)
		if err != nil {
			failures++
			if failures >= 3 {
				return errors.Join(err, errors.New("recovery switch retry budget was exhausted"))
			}
			expect, err = recoverGenerationFailure(ctx, cfg, owned.runtime, err, generationRecoveryExpectation{allowReturn: true}, &previous)
			if err != nil {
				return err
			}
			continue
		}
		state := input.state
		expect = generationRecoveryExpectation{state: &state, allowReturn: true}
	}
}

func newHTTPServer(address string, handler http.Handler, logger *slog.Logger) *http.Server {
	return &http.Server{
		Addr: address, Handler: handler, ErrorLog: slog.NewLogLogger(logger.Handler(), slog.LevelError),
		ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 1 << 20,
	}
}
