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
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/server"
)

var version = "0.1.0-dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	pool, err := database.Open(startup, cfg.DatabaseURL)
	if err != nil {
		return errors.New("PostgreSQL connection failed; check database availability and GOBY_DATABASE_URL")
	}
	defer pool.Close()
	if err := database.Migrate(startup, pool); err != nil {
		return err
	}
	store := identity.New(pool)
	initialized, err := store.Initialized(startup)
	if err != nil {
		return err
	}
	if !initialized && cfg.SetupToken == "" {
		return errors.New("GOBY_SETUP_TOKEN is required until the first administrator is created")
	}
	app, err := server.New(startup, cfg, pool, store, logger, version)
	if err != nil {
		return err
	}
	defer func() {
		closeCtx, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		if err := app.Close(closeCtx); err != nil {
			logger.Error("background work did not close cleanly")
		}
	}()
	srv := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           app.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	failure := make(chan error, 1)
	go func() { failure <- srv.ListenAndServe() }()
	logger.Info("server listening", "address", cfg.ListenAddress, "version", version, "database", "postgresql")
	select {
	case err := <-failure:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutdown, done := context.WithTimeout(context.Background(), 15*time.Second)
		defer done()
		return srv.Shutdown(shutdown)
	}
	return nil
}
