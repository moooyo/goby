package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/diagnostics"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/lifecycle"
	"github.com/moooyo/goby/internal/recovery"
	"github.com/moooyo/goby/internal/server"
)

const generationShutdownTimeout = 15 * time.Second

// A prepared generation owns its application, database resources and reserved
// listener, but serves no clients until Start. Runtime and diagnostics remain
// process-owned and must outlive this generation's complete Close pipeline.
type generation struct {
	cfg             config.Config
	pool            *pgxpool.Pool
	lease           *database.Lease
	manager         *recovery.Manager
	app             *server.Server
	server          *http.Server
	listener        net.Listener
	ctx             context.Context
	cancel          context.CancelFunc
	state           lifecycle.State
	pendingSwitch   string
	switchActivated bool

	mu          sync.Mutex
	started     bool
	stopping    bool
	serveResult chan error
	serveDone   chan struct{}
	watchDone   chan struct{}

	stopOnce         sync.Once
	stopDone         chan struct{}
	stopErr          error
	stopGraceErr     error
	stopResourceErr  error
	appOnce          sync.Once
	appDone          chan struct{}
	appErr           error
	closeOnce        sync.Once
	closeDone        chan struct{}
	closeErr         error
	resourceCloseErr error
}

// On failure, a non-nil result is only a cleanup handle: its context is already
// cancelled and Start cannot serve. It is returned only when cleanup outlives
// the bounded failure wait, so the coordinator can join it before Runtime.Close.
func prepareGeneration(ctx context.Context, runtime *recovery.Runtime, logger *slog.Logger, diag *diagnostics.Store, version string,
	suppliedPool *pgxpool.Pool, suppliedLease *database.Lease) (prepared *generation, resultErr error) {
	// Ownership transfers at entry, including on invalid or failed startup.
	// A private child context prevents one failed generation from cancelling
	// the signal context needed to prepare a retained recovery generation.
	parent := ctx
	if parent == nil {
		parent = context.Background()
	}
	lifetime, cancelLifetime := context.WithCancel(parent)
	g := &generation{pool: suppliedPool, lease: suppliedLease, ctx: lifetime, cancel: cancelLifetime}
	defer func() {
		if recover() != nil {
			prepared = nil
			resultErr = errors.New("application generation preparation failed unexpectedly")
		}
		if prepared == nil {
			cleanup, cancel := context.WithTimeout(context.Background(), generationShutdownTimeout)
			defer cancel()
			resultErr = errors.Join(resultErr, g.Close(cleanup))
			select {
			case <-g.closeDone:
			default:
				prepared = g
			}
		}
	}()
	if ctx == nil || runtime == nil || logger == nil || (suppliedPool == nil) != (suppliedLease == nil) {
		return nil, errors.New("application generation dependencies are invalid")
	}
	if g.lease != nil {
		if !g.lease.Protects(g.pool) {
			return nil, database.ErrLeaseUnavailable
		}
		g.watchLease()
	}
	startedAt := time.Now()
	if err := g.ctx.Err(); err != nil {
		return nil, err
	}
	cfg, state, err := runtime.ActiveConfig(g.ctx)
	if err != nil {
		return nil, generationError("active generation configuration is unavailable", err)
	}
	g.cfg = cfg
	g.state = state
	if cfg.StartupTimeout < time.Second || cfg.StartupTimeout > 30*time.Minute {
		return nil, errors.New("application generation startup timeout is invalid")
	}
	startup, cancelStartup := context.WithDeadline(g.ctx, startedAt.Add(cfg.StartupTimeout))
	// NewManager intentionally receives the generation lifetime, while its
	// constructor must still stop if preparation runs out of time. Remove this
	// temporary bridge before cancelling the successful startup context.
	startupCancelled := make(chan struct{})
	stopStartupBridge := context.AfterFunc(startup, func() { defer close(startupCancelled); g.cancel() })
	var bridgeOnce sync.Once
	stopBridge := func() {
		bridgeOnce.Do(func() {
			if !stopStartupBridge() {
				<-startupCancelled
			}
		})
	}
	defer func() { stopBridge(); cancelStartup() }()
	if err := startup.Err(); err != nil {
		return nil, err
	}
	if g.pool == nil {
		g.pool, err = database.Open(startup, cfg.DatabaseURL)
		if err != nil {
			return nil, generationError("PostgreSQL connection failed; check database availability and deployment configuration", err)
		}
		g.lease, err = database.AcquireLease(startup, g.pool)
		if err != nil {
			return nil, generationError("database generation ownership could not be acquired", err)
		}
		g.watchLease()
	}
	if err := g.checkStartup(startup); err != nil {
		return nil, err
	}
	// The retained slot marker must be checked before any migration can write
	// to the selected database. Bind only the successfully migrated generation.
	if err := runtime.CheckDatabase(startup, cfg, g.pool, g.lease); err != nil {
		return nil, generationError("database generation identity check failed", err)
	}
	if err := g.checkStartup(startup); err != nil {
		return nil, err
	}
	if err := database.Migrate(startup, g.pool); err != nil {
		return nil, generationError("database schema initialization failed", err)
	}
	if err := g.checkStartup(startup); err != nil {
		return nil, err
	}
	if err := runtime.BindDatabase(startup, cfg, g.pool, g.lease); err != nil {
		return nil, generationError("database generation binding failed", err)
	}
	if err := g.checkStartup(startup); err != nil {
		return nil, err
	}
	vault := identity.NewApplicationKeyVault(cfg.APIKeyMasterKeyFile)
	users := identity.NewWithApplicationKeyVault(g.pool, vault)
	initialized, err := users.Initialized(startup)
	if err != nil {
		return nil, generationError("administrator initialization state is unavailable", err)
	}
	if !initialized && cfg.SetupToken == "" {
		return nil, errors.New("GOBY_SETUP_TOKEN is required until the first administrator is created")
	}
	if err := g.checkStartup(startup); err != nil {
		return nil, err
	}
	g.manager, err = recovery.NewManager(g.ctx, runtime, cfg, g.pool, g.lease, vault, version)
	if err != nil {
		return nil, generationError("backup and recovery manager initialization failed", err)
	}
	if err := g.checkStartup(startup); err != nil {
		return nil, err
	}
	if err := g.manager.Reconcile(startup); err != nil {
		return nil, generationError("backup and recovery startup reconciliation failed", err)
	}
	if err := g.checkStartup(startup); err != nil {
		return nil, err
	}
	g.pendingSwitch, g.switchActivated, err = g.manager.PendingSwitch(startup)
	if err != nil {
		return nil, generationError("pending recovery transition could not be resolved", err)
	}
	if g.pendingSwitch != "" && !g.switchActivated {
		// A retiring source must remain free of scheduler, retention and media
		// startup writers until PrepareSwitch captures its retained facts.
		stopBridge()
		if err := g.checkStartup(startup); err != nil {
			return nil, err
		}
		return g, nil
	}
	if g.switchActivated {
		// Validate the exact staged facts before application initialization can
		// write. The manager durably records that initialization has begun.
		if err := g.manager.ValidateSwitch(startup); err != nil {
			return nil, generationError("activated recovery generation validation failed", err)
		}
		if err := g.checkStartup(startup); err != nil {
			return nil, err
		}
	}
	// Server.New and its transcode manager use this argument for initialization
	// only; their workers are explicitly drained by CloseApplication.
	g.app, err = server.New(startup, cfg, g.pool, users, logger, version, server.WithDiagnostics(diag), server.WithRecovery(g.manager))
	if err != nil {
		return nil, generationError("application generation initialization failed", err)
	}
	if err := g.checkStartup(startup); err != nil {
		return nil, err
	}
	g.server = newHTTPServer(cfg.ListenAddress, g.app.Handler(), logger)
	g.server.BaseContext = func(net.Listener) context.Context { return g.ctx }
	listen := net.ListenConfig{}
	g.listener, err = listen.Listen(startup, "tcp", cfg.ListenAddress)
	if err != nil {
		return nil, generationError("HTTP listener reservation failed", err)
	}
	if err := g.checkStartup(startup); err != nil {
		return nil, err
	}
	stopBridge()
	if err := g.checkStartup(startup); err != nil {
		return nil, err
	}
	return g, nil
}

func (g *generation) watchLease() {
	g.watchDone = make(chan struct{})
	go func() {
		defer close(g.watchDone)
		defer func() {
			if recover() != nil {
				g.cancel()
			}
		}()
		select {
		case <-g.lease.Done():
			g.cancel()
		case <-g.ctx.Done():
		}
	}()
}

func (g *generation) checkStartup(ctx context.Context) error {
	if !g.lease.Protects(g.pool) {
		return database.ErrLeaseUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return g.ctx.Err()
}

// Start may be called repeatedly, but starts Serve only once and always returns
// the same result channel. An already stopped generation never accepts clients.
func (g *generation) Start() <-chan error {
	if g == nil {
		result := make(chan error, 1)
		result <- errors.New("application generation is unavailable")
		close(result)
		return result
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.serveResult != nil {
		return g.serveResult
	}
	g.serveResult, g.serveDone = make(chan error, 1), make(chan struct{})
	var err error
	switch {
	case g.pendingSwitch != "":
		err = recovery.ErrConflict
	case g.stopping || g.server == nil || g.listener == nil:
		err = http.ErrServerClosed
	case g.ctx.Err() != nil:
		err = g.ctx.Err()
	case !g.lease.Protects(g.pool):
		err = database.ErrLeaseUnavailable
	}
	if err != nil {
		g.serveResult <- err
		close(g.serveResult)
		close(g.serveDone)
		return g.serveResult
	}
	g.started = true
	go func() {
		var serveErr error
		defer func() {
			if recover() != nil {
				serveErr = errors.New("HTTP generation stopped unexpectedly")
			}
			if !errors.Is(serveErr, http.ErrServerClosed) {
				g.cancel()
			}
			g.serveResult <- serveErr
			close(g.serveResult)
			close(g.serveDone)
		}()
		serveErr = g.server.Serve(g.listener)
		if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			serveErr = generationError("HTTP generation listener failed", serveErr)
		}
	}()
	return g.serveResult
}

// StopServing stops ingress and closes ordinary HTTP connections. It leaves the
// application, recovery manager and database fence available to the coordinator.
func (g *generation) StopServing(ctx context.Context) error {
	if g == nil {
		return nil
	}
	g.stopOnce.Do(func() {
		g.stopDone = make(chan struct{})
		g.mu.Lock()
		g.stopping = true
		started, serveDone := g.started, g.serveDone
		g.mu.Unlock()
		go func() {
			defer close(g.stopDone)
			if g.server != nil {
				shutdown, cancel := context.WithTimeout(ctx, generationShutdownTimeout)
				g.stopGraceErr = generationCall("HTTP generation shutdown failed", func() error { return g.server.Shutdown(shutdown) })
				cancel()
				g.stopResourceErr = generationCall("HTTP generation connection close failed", g.server.Close)
			}
			// Put Server into its shutdown state before closing an unserved
			// reservation. Otherwise Serve can report a raw accept failure and
			// incorrectly cancel the manager during an intentional ingress stop.
			g.stopResourceErr = errors.Join(g.stopResourceErr, generationCall("HTTP listener close failed", func() error {
				if g.listener == nil {
					return nil
				}
				err := g.listener.Close()
				if errors.Is(err, net.ErrClosed) {
					return nil
				}
				return err
			}))
			if started {
				<-serveDone
			}
			g.stopErr = errors.Join(g.stopGraceErr, g.stopResourceErr)
		}()
	})
	return waitGenerationClose(ctx, g.stopDone, func() error { return g.stopErr })
}

// A caller timeout bounds waiting, not resource ownership. The single worker
// keeps draining the application, and a later call joins that same completion.
func (g *generation) CloseApplication(ctx context.Context) error {
	if g == nil {
		return nil
	}
	g.appOnce.Do(func() {
		g.appDone = make(chan struct{})
		go func() {
			defer close(g.appDone)
			g.appErr = generationCall("application generation cleanup failed", func() error {
				if g.app == nil {
					return nil
				}
				return g.app.Close(context.Background())
			})
		}()
	})
	return waitGenerationClose(ctx, g.appDone, func() error { return g.appErr })
}

// Close starts one complete cleanup pipeline. It retains the lease until both
// application workers and every borrowed pool connection have drained. Lease
// connections are hijacked out of the pool, so closing the pool first does not
// wait for its own ownership connection. Shared Runtime must not close before
// this pipeline completes; after a timeout, a later Close may join it again.
func (g *generation) Close(ctx context.Context) error {
	if g == nil {
		return nil
	}
	g.closeOnce.Do(func() {
		g.closeDone = make(chan struct{})
		g.cancel()
		go func() {
			defer close(g.closeDone)
			_ = g.StopServing(ctx)
			<-g.stopDone
			g.closeErr = g.stopErr
			g.resourceCloseErr = generationCall("backup and recovery cleanup failed", func() error {
				if g.manager == nil {
					return nil
				}
				return g.manager.Close(context.Background())
			})
			g.resourceCloseErr = errors.Join(g.resourceCloseErr, g.CloseApplication(context.Background()))
			g.resourceCloseErr = errors.Join(g.resourceCloseErr, generationCall("database pool cleanup failed", func() error {
				if g.pool != nil {
					g.pool.Close()
				}
				return nil
			}))
			g.resourceCloseErr = errors.Join(g.resourceCloseErr, generationCall("database generation ownership release failed", func() error {
				if g.lease == nil {
					return nil
				}
				return g.lease.Close()
			}))
			if g.watchDone != nil {
				<-g.watchDone
			}
			g.closeErr = errors.Join(g.stopErr, g.resourceCloseErr)
		}()
	})
	return waitGenerationClose(ctx, g.closeDone, func() error { return g.closeErr })
}

func waitGenerationClose(ctx context.Context, done <-chan struct{}, result func() error) error {
	select {
	case <-done:
		return result()
	default:
	}
	select {
	case <-done:
		return result()
	case <-ctx.Done():
		select {
		case <-done:
			return result()
		default:
			return ctx.Err()
		}
	}
}

// Keep fixed sentinel classification without carrying private SQL, DSNs,
// configuration paths or unexpected panic payloads into the main error log.
func generationError(stage string, err error) error {
	if err == nil {
		return nil
	}
	for _, known := range []error{context.Canceled, context.DeadlineExceeded, database.ErrLeaseBusy, database.ErrLeaseUnavailable,
		http.ErrServerClosed, recovery.ErrInvalid, recovery.ErrUnavailable, recovery.ErrConflict, recovery.ErrBusy} {
		if errors.Is(err, known) {
			return errors.Join(errors.New(stage), known)
		}
	}
	return errors.New(stage)
}

func generationCall(stage string, call func() error) (result error) {
	defer func() {
		if recover() != nil {
			result = errors.New(stage)
		}
	}()
	return generationError(stage, call())
}
