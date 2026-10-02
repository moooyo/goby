package main

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/database"
)

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
	borrowed.Release()
	if err := g.Close(ctx); err != nil || lease.Protects(data) {
		t.Fatal("generation cleanup did not join both pool drains before releasing ownership")
	}
	if data.Stat().TotalConns() != 0 || control.Stat().TotalConns() != 0 {
		t.Fatal("generation cleanup retained application database backends")
	}
}
