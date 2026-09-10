package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/recovery"
)

type generationTestListener struct {
	accepts      atomic.Int32
	closed       chan struct{}
	closeStarted chan struct{}
	releaseClose chan struct{}
	startOnce    sync.Once
	closeOnce    sync.Once
}

func newGenerationTestListener(blockClose bool) *generationTestListener {
	listener := &generationTestListener{closed: make(chan struct{}), closeStarted: make(chan struct{}), releaseClose: make(chan struct{})}
	if !blockClose {
		close(listener.releaseClose)
	}
	return listener
}

func (l *generationTestListener) Accept() (net.Conn, error) {
	l.accepts.Add(1)
	<-l.closed
	return nil, net.ErrClosed
}
func (l *generationTestListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}
}
func (l *generationTestListener) Close() error {
	l.startOnce.Do(func() { close(l.closeStarted) })
	<-l.releaseClose
	l.closeOnce.Do(func() { close(l.closed) })
	return nil
}

func generationTestReserved(listener net.Listener) *generation {
	ctx, cancel := context.WithCancel(context.Background())
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &generation{ctx: ctx, cancel: cancel, listener: listener, server: newHTTPServer("127.0.0.1:12345", http.NotFoundHandler(), logger)}
}

func TestGenerationPendingAcceptanceNeverStartsItsReservedListener(t *testing.T) {
	listener := newGenerationTestListener(false)
	g := generationTestReserved(listener)
	g.pendingSwitch, g.switchActivated = "0123456789abcdef0123456789abcdef", true
	result := g.Start()
	if result != g.Start() || !errors.Is(<-result, recovery.ErrConflict) || listener.accepts.Load() != 0 {
		t.Fatal("an unaccepted generation started serving or repeated Start created another result")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := g.Close(ctx); err != nil {
		t.Fatal("close unserved acceptance reservation")
	}
}

func TestGenerationStopServingClosesAnUnservedReservationWithoutCancellingManagerLifetime(t *testing.T) {
	listener := newGenerationTestListener(false)
	g := generationTestReserved(listener)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := g.StopServing(ctx); err != nil {
		t.Fatal("stop unserved HTTP reservation")
	}
	if g.ctx.Err() != nil || listener.accepts.Load() != 0 {
		t.Fatal("stopping ingress cancelled the generation or accepted a client")
	}
	select {
	case <-listener.closed:
	default:
		t.Fatal("unserved listener remains reserved")
	}
	if err := <-g.Start(); !errors.Is(err, http.ErrServerClosed) {
		t.Fatal("a stopped reservation became serveable again")
	}
	if err := g.Close(ctx); err != nil || g.ctx.Err() == nil {
		t.Fatal("complete cleanup did not cancel the generation lifetime")
	}
}

func TestGenerationCloseTimeoutRetainsAndJoinsItsSingleCleanupPipeline(t *testing.T) {
	listener := newGenerationTestListener(true)
	g := generationTestReserved(listener)
	ctx, cancel := context.WithCancel(context.Background())
	finished := make(chan error, 1)
	go func() { finished <- g.Close(ctx) }()
	<-listener.closeStarted
	cancel()
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("bounded cleanup did not return its caller cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cleanup ignored its caller budget")
	}
	select {
	case <-g.closeDone:
		t.Fatal("cleanup completed before its reserved resource was released")
	default:
	}
	close(listener.releaseClose)
	joined, stop := context.WithTimeout(context.Background(), 2*time.Second)
	defer stop()
	if err := g.Close(joined); err != nil {
		t.Fatal("later cleanup did not join the original pipeline")
	}
	select {
	case <-g.closeDone:
	default:
		t.Fatal("cleanup completion was not observable after join")
	}
}

func TestGenerationErrorsRetainOnlySafeClassificationAndSuppressPanicPayloads(t *testing.T) {
	const secret = "postgres://private-user:private-secret@private.invalid/private-path"
	err := generationError("generation initialization failed", fmt.Errorf("%s: %w", secret, database.ErrLeaseUnavailable))
	if !errors.Is(err, database.ErrLeaseUnavailable) || err.Error() != "generation initialization failed\n"+database.ErrLeaseUnavailable.Error() {
		t.Fatal("generation failure lost its safe classification or retained private connection data")
	}
	err = generationCall("generation cleanup failed", func() error { panic(secret) })
	if err == nil || err.Error() != "generation cleanup failed" {
		t.Fatal("generation cleanup exposed a panic payload")
	}
}
