package literaldial

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

type trackedConnection struct {
	net.Conn
	closed atomic.Int32
}

type unpublishedDeadlineContext struct {
	context.Context
	deadline time.Time
}

func (ctx unpublishedDeadlineContext) Deadline() (time.Time, bool) {
	return ctx.deadline, true
}

func (connection *trackedConnection) Close() error {
	connection.closed.Add(1)
	return nil
}

func testAddresses() []netip.Addr {
	return []netip.Addr{
		netip.MustParseAddr("1.1.1.1"),
		netip.MustParseAddr("2606:4700:4700::1111"),
		netip.MustParseAddr("8.8.8.8"),
		netip.MustParseAddr("8.8.4.4"),
	}
}

func TestDialContextStaggeredSuccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		winner := &trackedConnection{}
		var attempts, retired atomic.Int32
		connection, err := DialContext(context.Background(), "tcp", testAddresses(), "443", func(ctx context.Context, network, address string) (net.Conn, error) {
			attempts.Add(1)
			defer retired.Add(1)
			if network != "tcp" {
				t.Errorf("network = %q", network)
			}
			switch address {
			case "1.1.1.1:443":
				<-ctx.Done()
				return nil, ctx.Err()
			case "[2606:4700:4700::1111]:443":
				if elapsed := time.Since(start); elapsed != staggerDelay {
					t.Errorf("fallback delay = %v, want %v", elapsed, staggerDelay)
				}
				return winner, nil
			default:
				t.Errorf("unexpected literal dial: %q", address)
				return nil, errors.New("unexpected address")
			}
		})
		if err != nil || connection != winner || attempts.Load() != 2 || retired.Load() != 2 || winner.closed.Load() != 0 {
			t.Fatalf("connection = %v, error = %v, attempts = %d, retired = %d, winner closes = %d", connection, err, attempts.Load(), retired.Load(), winner.closed.Load())
		}
	})
}

func TestDialContextBoundsConcurrencyAndJoinsCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		release := make(chan struct{})
		var attempts, active, canceled atomic.Int32
		late := make(chan *trackedConnection, maxConcurrent)
		done := make(chan result, 1)
		go func() {
			connection, err := DialContext(ctx, "tcp", testAddresses(), "443", func(ctx context.Context, _, _ string) (net.Conn, error) {
				attempts.Add(1)
				active.Add(1)
				defer active.Add(-1)
				<-ctx.Done()
				canceled.Add(1)
				<-release
				connection := &trackedConnection{}
				late <- connection
				return connection, nil
			})
			done <- result{connection: connection, err: err}
		}()
		synctest.Wait()
		if attempts.Load() != 1 {
			t.Fatalf("initial attempts = %d", attempts.Load())
		}
		time.Sleep(4 * staggerDelay)
		synctest.Wait()
		if attempts.Load() != maxConcurrent || active.Load() != maxConcurrent {
			t.Fatalf("attempts = %d, active = %d", attempts.Load(), active.Load())
		}
		cancel()
		synctest.Wait()
		if canceled.Load() != maxConcurrent {
			t.Fatalf("canceled attempts = %d", canceled.Load())
		}
		select {
		case completed := <-done:
			t.Fatalf("returned before attempts retired: %v", completed.err)
		default:
		}
		close(release)
		completed := <-done
		if completed.connection != nil || !errors.Is(completed.err, context.Canceled) || active.Load() != 0 || attempts.Load() != maxConcurrent {
			t.Fatalf("result = %+v, active = %d, attempts = %d", completed, active.Load(), attempts.Load())
		}
		for range maxConcurrent {
			if connection := <-late; connection.closed.Load() != 1 {
				t.Fatalf("late connection closes = %d", connection.closed.Load())
			}
		}
	})
}

func TestDialContextClosesLateSuccessfulLoser(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		winner, loser := &trackedConnection{}, &trackedConnection{}
		release := make(chan struct{})
		canceled := make(chan struct{})
		done := make(chan result, 1)
		go func() {
			connection, err := DialContext(context.Background(), "tcp", testAddresses()[:2], "443", func(ctx context.Context, _, address string) (net.Conn, error) {
				if address == "1.1.1.1:443" {
					<-ctx.Done()
					close(canceled)
					<-release
					return loser, nil
				}
				return winner, nil
			})
			done <- result{connection: connection, err: err}
		}()
		<-canceled
		synctest.Wait()
		select {
		case completed := <-done:
			t.Fatalf("returned before loser retired: %v", completed.err)
		default:
		}
		close(release)
		completed := <-done
		if completed.err != nil || completed.connection != winner || winner.closed.Load() != 0 || loser.closed.Load() != 1 {
			t.Fatalf("result = %+v, winner closes = %d, loser closes = %d", completed, winner.closed.Load(), loser.closed.Load())
		}
	})
}

func TestDialContextAllFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		failure := errors.New("connection refused")
		var attempts atomic.Int32
		start := time.Now()
		connection, err := DialContext(context.Background(), "tcp", testAddresses(), "443", func(context.Context, string, string) (net.Conn, error) {
			attempts.Add(1)
			return nil, failure
		})
		if connection != nil || !errors.Is(err, failure) || attempts.Load() != int32(len(testAddresses())) || time.Since(start) != 0 {
			t.Fatalf("connection = %v, error = %v, attempts = %d, elapsed = %v", connection, err, attempts.Load(), time.Since(start))
		}
	})
}

func TestDialContextProgressesPastTwoStalledAddresses(t *testing.T) {
	for _, test := range []struct {
		name    string
		budget  time.Duration
		elapsed time.Duration
	}{
		{name: "webhook", budget: 3 * time.Second, elapsed: 2250 * time.Millisecond},
		{name: "provider", budget: 10 * time.Second, elapsed: 5125 * time.Millisecond},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				start := time.Now()
				ctx, cancel := context.WithTimeout(context.Background(), test.budget)
				defer cancel()
				winner, expired := &trackedConnection{}, &trackedConnection{}
				var attempts, active atomic.Int32
				connection, err := DialContext(ctx, "tcp", testAddresses()[:3], "443", func(ctx context.Context, _, address string) (net.Conn, error) {
					attempts.Add(1)
					if count := active.Add(1); count > maxConcurrent {
						t.Errorf("active attempts = %d", count)
					}
					defer active.Add(-1)
					if address == "8.8.8.8:443" {
						return winner, nil
					}
					<-ctx.Done()
					if address == "[2606:4700:4700::1111]:443" {
						return expired, nil
					}
					return nil, ctx.Err()
				})
				if err != nil || connection != winner || attempts.Load() != 3 || active.Load() != 0 || time.Since(start) != test.elapsed || expired.closed.Load() != 1 {
					t.Fatalf("connection = %v, error = %v, attempts = %d, active = %d, elapsed = %v, expired closes = %d", connection, err, attempts.Load(), active.Load(), time.Since(start), expired.closed.Load())
				}
			})
		})
	}
}

func TestDialContextClosesConnectionsReturnedWithErrors(t *testing.T) {
	failed := &trackedConnection{}
	failure := errors.New("connection failed after opening")
	connection, err := DialContext(context.Background(), "tcp", testAddresses()[:1], "443", func(context.Context, string, string) (net.Conn, error) {
		return failed, failure
	})
	if connection != nil || !errors.Is(err, failure) || failed.closed.Load() != 1 {
		t.Fatalf("connection = %v, error = %v, closes = %d", connection, err, failed.closed.Load())
	}
}

func TestDialContextShortBudgetBoundsAttemptsAndRetires(t *testing.T) {
	for _, test := range []struct {
		name   string
		count  int
		budget time.Duration
	}{
		{name: "three addresses after slow DNS", count: 3, budget: 400 * time.Millisecond},
		{name: "32 addresses after slow DNS", count: 32, budget: 400 * time.Millisecond},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				addresses := make([]netip.Addr, test.count)
				for index := range addresses {
					addresses[index] = netip.AddrFrom4([4]byte{1, 1, 1, byte(index + 1)})
				}
				start := time.Now()
				ctx, cancel := context.WithTimeout(context.Background(), test.budget)
				defer cancel()
				var attempts, active atomic.Int32
				connection, err := DialContext(ctx, "tcp", addresses, "443", func(ctx context.Context, _, _ string) (net.Conn, error) {
					attempts.Add(1)
					if count := active.Add(1); count > maxConcurrent {
						t.Errorf("active attempts = %d", count)
					}
					defer active.Add(-1)
					<-ctx.Done()
					return nil, ctx.Err()
				})
				if !errors.Is(err, context.DeadlineExceeded) || connection != nil || attempts.Load() != 2 || active.Load() != 0 || time.Since(start) != test.budget {
					t.Fatalf("connection = %v, error = %v, attempts = %d, active = %d, elapsed = %v", connection, err, attempts.Load(), active.Load(), time.Since(start))
				}
			})
		})
	}
}

func TestDialContextPreservesHealthyPreferredAddress(t *testing.T) {
	for _, test := range []struct {
		name         string
		latency      time.Duration
		allHealthy   bool
		wantAttempts int32
	}{
		{name: "32 healthy addresses need 200ms", latency: 200 * time.Millisecond, allHealthy: true, wantAttempts: 1},
		{name: "slow preferred address retains full budget", latency: 2750 * time.Millisecond, wantAttempts: 3},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				addresses := make([]netip.Addr, 32)
				for index := range addresses {
					addresses[index] = netip.AddrFrom4([4]byte{1, 1, 1, byte(index + 1)})
				}
				start := time.Now()
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				winner := &trackedConnection{}
				var attempts, active atomic.Int32
				connection, err := DialContext(ctx, "tcp", addresses, "443", func(ctx context.Context, _, address string) (net.Conn, error) {
					attempts.Add(1)
					if count := active.Add(1); count > maxConcurrent {
						t.Errorf("active attempts = %d", count)
					}
					defer active.Add(-1)
					if test.allHealthy || address == "1.1.1.1:443" {
						timer := time.NewTimer(test.latency)
						defer timer.Stop()
						select {
						case <-timer.C:
							return winner, nil
						case <-ctx.Done():
							return nil, ctx.Err()
						}
					}
					<-ctx.Done()
					return nil, ctx.Err()
				})
				if err != nil || connection != winner || attempts.Load() != test.wantAttempts || active.Load() != 0 || time.Since(start) != test.latency || winner.closed.Load() != 0 {
					t.Fatalf("connection = %v, error = %v, attempts = %d, active = %d, elapsed = %v, winner closes = %d", connection, err, attempts.Load(), active.Load(), time.Since(start), winner.closed.Load())
				}
			})
		})
	}
}

func TestDialContextUsesOneDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		var attempts atomic.Int32
		connection, err := DialContext(ctx, "tcp", testAddresses()[:2], "443", func(ctx context.Context, _, _ string) (net.Conn, error) {
			attempts.Add(1)
			if deadline, ok := ctx.Deadline(); !ok || !deadline.Equal(start.Add(time.Second)) {
				t.Errorf("attempt deadline = %v, present = %v", deadline, ok)
			}
			<-ctx.Done()
			return nil, ctx.Err()
		})
		if connection != nil || !errors.Is(err, context.DeadlineExceeded) || time.Since(start) != time.Second || attempts.Load() != maxConcurrent {
			t.Fatalf("connection = %v, error = %v, elapsed = %v, attempts = %d", connection, err, time.Since(start), attempts.Load())
		}
		connection, err = DialContext(ctx, "tcp", testAddresses(), "443", func(context.Context, string, string) (net.Conn, error) {
			t.Error("expired context started another attempt")
			return nil, errors.New("unexpected attempt")
		})
		if connection != nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expired result = %v, %v", connection, err)
		}
	})
}

func TestDialContextRejectsInvalidLiteralBeforeDialing(t *testing.T) {
	for _, addresses := range [][]netip.Addr{nil, {netip.MustParseAddr("1.1.1.1"), {}}} {
		connection, err := DialContext(context.Background(), "tcp", addresses, "443", func(context.Context, string, string) (net.Conn, error) {
			t.Error("invalid answer reached the dialer")
			return nil, errors.New("unexpected attempt")
		})
		if err == nil || connection != nil {
			t.Fatalf("invalid literal result = %v, %v", connection, err)
		}
	}
}

func TestDialContextRejectsUnpublishedExpiredDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := unpublishedDeadlineContext{Context: context.Background(), deadline: time.Now()}
		if ctx.Err() != nil {
			t.Fatal("fixture already published cancellation")
		}
		connection, err := DialContext(ctx, "tcp", testAddresses(), "443", func(context.Context, string, string) (net.Conn, error) {
			t.Error("elapsed deadline started another attempt before cancellation publication")
			return nil, errors.New("unexpected attempt")
		})
		if connection != nil || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("connection = %v, error = %v", connection, err)
		}
	})
}
