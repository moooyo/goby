package main

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/puddle/v2"
	"github.com/moooyo/goby/internal/database"
)

func assertGenerationPoolClosed(t *testing.T, name string, pool *pgxpool.Pool) {
	t.Helper()
	// A spent fixture context would mask ErrClosedPool with its own deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, err := pool.Acquire(ctx)
	if connection != nil {
		connection.Release()
	}
	if !errors.Is(err, puddle.ErrClosedPool) {
		stats := pool.Stat()
		t.Fatalf("closed %s pool returned %v instead of ErrClosedPool: total=%d idle=%d acquired=%d",
			name, err, stats.TotalConns(), stats.IdleConns(), stats.AcquiredConns())
	}
}

func TestGenerationCloseRetainsLeaseUntilReservedControlCapacityDrains(t *testing.T) {
	url := os.Getenv("GOBY_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("GOBY_TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	data, err := database.Open(ctx, url)
	if err != nil {
		t.Fatal("open the bounded generation data pool")
	}
	t.Cleanup(data.Close)
	lease, err := database.AcquireLease(ctx, data)
	if err != nil {
		t.Fatal("acquire the generation deployment lease")
	}
	t.Cleanup(func() { _ = lease.Close() })
	control, err := database.OpenPlaybackControlFor(ctx, data)
	if err != nil {
		t.Fatal("open the generation playback-control pool")
	}
	t.Cleanup(control.Close)
	borrowed, err := control.Acquire(ctx)
	if err != nil {
		t.Fatal("borrow reserved capacity before shutdown")
	}
	t.Cleanup(borrowed.Release)
	g := generationTestReserved(newGenerationTestListener(false))
	g.pool, g.playbackControlPool, g.lease = data, control, lease
	t.Cleanup(func() {
		borrowed.Release()
		if err := g.Close(context.Background()); err != nil {
			t.Error("join the owned generation cleanup")
		}
	})
	deadline, stop := context.WithTimeout(ctx, 50*time.Millisecond)
	err = g.Close(deadline)
	stop()
	if !errors.Is(err, context.DeadlineExceeded) || !lease.Protects(data) {
		t.Fatal("bounded Close released the lease before a borrowed control connection drained")
	}
	select {
	case <-g.closeDone:
		t.Fatal("generation cleanup completed while reserved capacity remained borrowed")
	default:
	}
	controlConn := borrowed.Conn()
	controlCleanupDone := controlConn.PgConn().CleanupDone()
	controlPID := controlConn.PgConn().PID()
	borrowed.Release()
	if err := g.Close(ctx); err != nil || lease.Protects(data) {
		t.Fatal("generation cleanup did not join both pool drains before releasing ownership")
	}
	// Closed pool counts can retain bookkeeping entries; check the concrete
	// resource and admission boundary instead of aggregate post-close counts.
	if !controlConn.IsClosed() {
		t.Fatalf("generation cleanup retained control connection PID %d: data_total=%d control_total=%d control_idle=%d",
			controlPID, data.Stat().TotalConns(), control.Stat().TotalConns(), control.Stat().IdleConns())
	}
	select {
	case <-controlCleanupDone:
	default:
		t.Fatalf("generation cleanup did not finish control connection PID %d cleanup: data_total=%d control_total=%d control_idle=%d",
			controlPID, data.Stat().TotalConns(), control.Stat().TotalConns(), control.Stat().IdleConns())
	}
	assertGenerationPoolClosed(t, "data", data)
	assertGenerationPoolClosed(t, "control", control)
}
