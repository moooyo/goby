package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moooyo/goby/internal/database"
	"github.com/moooyo/goby/internal/diagnostics"
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
	if !errors.Is(err, database.ErrLeaseUnavailable) || diagnostics.ErrorClass(err) != "database_lease_unavailable" || err.Error() != "database_lease_unavailable" {
		t.Fatal("generation failure lost its safe classification or retained private connection data")
	}
	err = generationCall("generation cleanup failed", func() error { panic(secret) })
	if err == nil || diagnostics.ErrorClass(err) != "panic" || err.Error() != "panic" {
		t.Fatal("generation cleanup exposed a panic payload")
	}
}

type generationHostileError struct{ calls *int }

func (err generationHostileError) Error() string {
	*err.calls++
	panic("private-error-payload")
}

func (err generationHostileError) Unwrap() error {
	*err.calls++
	panic("private-unwrap-payload")
}

func (err generationHostileError) Is(error) bool {
	*err.calls++
	panic("private-is-payload")
}

func TestGenerationSanitizationDoesNotExecuteUnknownErrorsAndKeepsSentinelPriority(t *testing.T) {
	calls := 0
	hostile := generationHostileError{calls: &calls}
	err := generationError("fixed generation stage", errors.Join(hostile, database.ErrLeaseUnavailable, context.Canceled))
	if calls != 0 || !errors.Is(err, context.Canceled) || errors.Is(err, database.ErrLeaseUnavailable) {
		t.Fatal("safe sentinel extraction changed its priority or evaluated an unknown error")
	}
	err = generationCall("fixed cleanup stage", func() error { panic(hostile) })
	if calls != 0 || diagnostics.ErrorClass(err) != "panic" {
		t.Fatal("panic classification evaluated or retained its payload")
	}
}

type generationFailureListener struct {
	panicOnAccept bool
	closed        atomic.Bool
}

func (listener *generationFailureListener) Accept() (net.Conn, error) {
	if listener.panicOnAccept {
		panic("private-accept-panic")
	}
	return nil, errors.New("private-accept-failure")
}

func (listener *generationFailureListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 12345}
}

func (listener *generationFailureListener) Close() error {
	listener.closed.Store(true)
	return nil
}

func TestGenerationStartRetainsFailureClassBeforePublishingResult(t *testing.T) {
	databaseURL := os.Getenv("GOBY_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("GOBY_TEST_DATABASE_URL is required for the generation lease regression")
	}
	// The deployment lease covers the test database, so these cases stay serial.
	for _, test := range []struct {
		class         string
		panicOnAccept bool
	}{{"listener_failed", false}, {"panic", true}} {
		t.Run(test.class, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			t.Cleanup(cancel)
			pool, err := pgxpool.New(ctx, databaseURL)
			if err != nil {
				t.Fatal("create isolated generation test database pool")
			}
			t.Cleanup(pool.Close)
			lease, err := database.AcquireLease(ctx, pool)
			if err != nil {
				t.Fatal("acquire isolated generation test deployment lease")
			}
			listener := &generationFailureListener{panicOnAccept: test.panicOnAccept}
			g := generationTestReserved(listener)
			g.pool, g.lease = pool, lease
			cancelled, release := make(chan struct{}), make(chan struct{})
			var cancelOnce, releaseOnce sync.Once
			releaseCancel := func() { releaseOnce.Do(func() { close(release) }) }
			originalCancel := g.cancel
			g.cancel = func() {
				originalCancel()
				cancelOnce.Do(func() {
					close(cancelled)
					<-release
				})
			}
			t.Cleanup(func() {
				releaseCancel()
				closeCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
				defer stop()
				if err := g.Close(closeCtx); err != nil {
					t.Error("complete generation cleanup after the Serve regression")
				}
			})
			result := g.Start()
			select {
			case <-cancelled:
			case <-time.After(5 * time.Second):
				t.Fatal("Serve did not cancel the generation after its listener failure")
			}
			select {
			case <-result:
				t.Fatal("Serve published its result before the blocked cancellation returned")
			default:
			}
			cause := errors.New("active application generation stopped")
			observed := g.failureError(cause)
			if g.ctx.Err() == nil || !errors.Is(observed, cause) || diagnostics.ErrorClass(observed) != test.class {
				t.Fatal("the cancellation path lost the actual Serve failure class or control cause")
			}
			releaseCancel()
			select {
			case err := <-result:
				if diagnostics.ErrorClass(err) != test.class {
					t.Fatal("the Serve result and cancellation path disagree on the safe failure class")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("Serve did not publish its result after cancellation was released")
			}
			closeCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
			defer stop()
			if err := g.Close(closeCtx); err != nil || !listener.closed.Load() {
				t.Fatal("the failed generation did not completely close its listener and resources")
			}
		})
	}
}
