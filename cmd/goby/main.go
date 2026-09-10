package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/diagnostics"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/server"
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
	defer func() {
		if recover() != nil {
			runErr = errors.New("server stopped unexpectedly")
		}
		// Application and database cleanup run before the final persistent event.
		if runErr != nil {
			logger.Error("server stopped", "error", runErr)
		} else if !shutdownStarted.IsZero() {
			logger.Info("server shutdown completed", "duration_ms", time.Since(shutdownStarted).Milliseconds())
		} else {
			logger.Info("server stopped")
		}
		slog.SetDefault(bootstrapLogger)
		if diagnosticStore != nil {
			if err := diagnosticStore.Close(); err != nil {
				bootstrapLogger.Error("background work did not close cleanly", "error", err)
				if runErr == nil {
					runErr = err
				}
			}
		}
	}()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	diagnosticStore, err = diagnostics.Open(cfg.Diagnostics)
	if err != nil {
		return err
	}
	// Both handlers share the raw fallback; nesting them would reject records.
	logger = slog.New(diagnostics.NewHandler(diagnosticStore, fallback))
	slog.SetDefault(logger)
	logger.Info("server starting", "version", version, "database", "postgresql")
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, cfg.StartupTimeout)
	defer cancel()
	pool, err := database.Open(startup, cfg.DatabaseURL)
	if err != nil {
		return errors.New("PostgreSQL connection failed; check database availability and GOBY_DATABASE_URL")
	}
	defer pool.Close()
	if err := database.Migrate(startup, pool); err != nil {
		return err
	}
	store := identity.NewWithApplicationKeyVault(pool, identity.NewApplicationKeyVault(cfg.APIKeyMasterKeyFile))
	initialized, err := store.Initialized(startup)
	if err != nil {
		return err
	}
	if !initialized && cfg.SetupToken == "" {
		return errors.New("GOBY_SETUP_TOKEN is required until the first administrator is created")
	}
	app, err := server.New(startup, cfg, pool, store, logger, version, server.WithDiagnostics(diagnosticStore))
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if err := app.Close(closeCtx); err != nil {
			logger.Error("background work did not close cleanly", "error", err)
			if runErr == nil {
				runErr = err
			}
		}
	}()
	srv := newHTTPServer(cfg.ListenAddress, app.Handler(), logger)
	defer func() {
		// Shutdown can time out with active connections; always close them before
		// application cleanup and before releasing the diagnostic store.
		if err := srv.Close(); err != nil {
			logger.Error("background work did not close cleanly", "error", err)
			if runErr == nil {
				runErr = err
			}
		}
	}()
	listener, err := net.Listen("tcp", cfg.ListenAddress)
	if err != nil {
		return err
	}
	failure := make(chan error, 1)
	go func() { failure <- srv.Serve(listener) }()
	logger.Info("server listening", "version", version, "database", "postgresql")
	select {
	case err := <-failure:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutdownStarted = time.Now()
		shutdown, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		return srv.Shutdown(shutdown)
	}
	return nil
}

func newHTTPServer(address string, handler http.Handler, logger *slog.Logger) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}
