// Package literaldial races bounded connection attempts without resolving hosts.
package literaldial

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"time"
)

const (
	maxConcurrent  = 2
	staggerDelay   = 250 * time.Millisecond
	minAttemptTime = 2 * time.Second
)

// DialFunc must honor context cancellation and return only after its connection
// attempt has stopped. DialContext joins every call before returning.
type DialFunc func(context.Context, string, string) (net.Conn, error)

type result struct {
	connection net.Conn
	err        error
}

// DialContext dials already authorized literal addresses under one caller-owned
// deadline. A slow attempt starts a fallback after a short delay; a failed
// attempt permits an immediate replacement. At most two attempts run at once.
// The preferred address retains the full deadline. Fallback attempts divide
// the remaining time across unstarted addresses with a two-second minimum,
// always capped by the total deadline. Short budgets need not try every address.
// The stagger is capped at half the attempt budget to leave time for a fallback.
// The caller must validate the complete answer before calling DialContext.
func DialContext(ctx context.Context, network string, addresses []netip.Addr, port string, dial DialFunc) (net.Conn, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, errors.New("no literal addresses to dial")
	}
	for _, address := range addresses {
		if !address.IsValid() {
			return nil, errors.New("invalid literal address")
		}
	}
	attemptContext, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan result, maxConcurrent)
	var workers sync.WaitGroup
	active, next := 0, 0
	start := func() time.Duration {
		ctx := attemptContext
		stop := func() {}
		delay := staggerDelay
		if deadline, ok := ctx.Deadline(); ok {
			remaining := time.Until(deadline)
			budget := remaining
			if next > 0 {
				budget = min(remaining, max(minAttemptTime, remaining/time.Duration(len(addresses)-next)))
			}
			if budget < remaining {
				ctx, stop = context.WithTimeout(ctx, budget)
			}
			delay = min(delay, budget/2)
		}
		address := net.JoinHostPort(addresses[next].String(), port)
		next++
		active++
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer stop()
			var connection net.Conn
			err := contextError(ctx)
			if err == nil {
				connection, err = dial(ctx, network, address)
			}
			if contextErr := contextError(ctx); contextErr != nil {
				err = contextErr
			}
			results <- result{connection: connection, err: err}
		}()
		return delay
	}
	finish := func(connection net.Conn, err error) (net.Conn, error) {
		cancel()
		for active > 0 {
			loser := <-results
			active--
			if loser.connection != nil {
				_ = loser.connection.Close()
			}
		}
		workers.Wait()
		if contextErr := contextError(ctx); contextErr != nil {
			if connection != nil {
				_ = connection.Close()
			}
			return nil, contextErr
		}
		return connection, err
	}
	timer := time.NewTimer(start())
	defer timer.Stop()
	for {
		var stagger <-chan time.Time
		if next < len(addresses) && active < maxConcurrent {
			stagger = timer.C
		}
		select {
		case <-ctx.Done():
			return finish(nil, ctx.Err())
		case completed := <-results:
			active--
			if completed.err == nil && completed.connection != nil {
				return finish(completed.connection, nil)
			}
			if completed.connection != nil {
				_ = completed.connection.Close()
			}
			if completed.err == nil {
				completed.err = errors.New("dial returned no connection")
			}
			if err := contextError(ctx); err != nil {
				return finish(nil, err)
			}
			if next < len(addresses) {
				timer.Reset(start())
			}
			if active == 0 {
				return finish(nil, completed.err)
			}
		case <-stagger:
			if err := contextError(ctx); err != nil {
				return finish(nil, err)
			}
			timer.Reset(start())
		}
	}
}

func contextError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// A deadline can pass before its cancellation timer publishes ctx.Err().
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}
