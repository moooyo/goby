package server

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func loginLimiterAllowAt(limiter *loginLimiter, key string, now time.Time) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	return limiter.allowLocked(key, now)
}

func loginLimiterFill(limiter *loginLimiter, count int, expires time.Time) {
	for index := 0; index < count; index++ {
		limiter.windows[fmt.Sprintf("address-%d", index)] = loginWindow{count: 10, expires: expires}
	}
}

func TestLoginLimiterWindowBoundaries(t *testing.T) {
	limiter := newLoginLimiter()
	start := time.Unix(1000, 0)
	for attempt := 1; attempt <= 10; attempt++ {
		if !loginLimiterAllowAt(limiter, "client", start) {
			t.Fatalf("attempt %d was denied before the limit", attempt)
		}
	}
	for _, now := range []time.Time{start, start.Add(-time.Hour), start.Add(time.Minute - time.Nanosecond), start.Add(time.Minute)} {
		if loginLimiterAllowAt(limiter, "client", now) {
			t.Fatalf("a limited client was allowed at %v", now)
		}
		if !limiter.windows["client"].expires.Equal(start.Add(time.Minute)) {
			t.Fatal("a denied request changed the window expiry")
		}
	}
	next := start.Add(time.Minute + time.Nanosecond)
	if !loginLimiterAllowAt(limiter, "client", next) {
		t.Fatal("the client did not recover after its window expired")
	}
	if window := limiter.windows["client"]; window.count != 1 || !window.expires.Equal(next.Add(time.Minute)) {
		t.Fatalf("the renewed window is invalid: %+v", window)
	}
}

func TestLoginLimiterCleanupCadenceAndFullCapacity(t *testing.T) {
	limiter := newLoginLimiter()
	start := time.Unix(1000, 0)
	expires := start.Add(time.Minute)
	loginLimiterFill(limiter, 20000, expires)
	if loginLimiterAllowAt(limiter, "address-0", start) {
		t.Fatal("a full window was allowed")
	}
	if !limiter.nextCleanup.Equal(start.Add(time.Second)) {
		t.Fatal("the initial cleanup did not schedule its next deadline")
	}
	// An expired sentinel inserted after the first pass proves that subsequent
	// denials do not traverse and clean the map before the maintenance deadline.
	limiter.windows["address-1"] = loginWindow{count: 10, expires: start.Add(-time.Nanosecond)}
	for attempt := 0; attempt < 1000; attempt++ {
		if loginLimiterAllowAt(limiter, "address-0", start.Add(time.Second-time.Nanosecond)) ||
			loginLimiterAllowAt(limiter, "new-client", start.Add(time.Second-time.Nanosecond)) {
			t.Fatal("cleanup pressure admitted a limited or excess address")
		}
	}
	if _, found := limiter.windows["address-1"]; !found || len(limiter.windows) != 20000 || !limiter.nextCleanup.Equal(start.Add(time.Second)) {
		t.Fatal("denied requests performed premature cleanup or exceeded capacity")
	}
	if !loginLimiterAllowAt(limiter, "new-client", start.Add(time.Second)) {
		t.Fatal("the next cleanup did not reclaim an expired slot for a new address")
	}
	if _, found := limiter.windows["address-1"]; found || len(limiter.windows) != 20000 {
		t.Fatal("cleanup did not replace exactly the expired address")
	}
	if window := limiter.windows["address-2"]; window.count != 10 || !window.expires.Equal(expires) {
		t.Fatal("cleanup changed a live address's limit")
	}
	if loginLimiterAllowAt(limiter, "another-client", start.Add(time.Second)) {
		t.Fatal("a new address displaced a live limit at capacity")
	}
}

func TestLoginLimiterExistingExpiredAddressRecoversBeforeCleanup(t *testing.T) {
	limiter := newLoginLimiter()
	start := time.Unix(1000, 0)
	loginLimiterFill(limiter, 20000, start.Add(time.Minute))
	limiter.nextCleanup = start.Add(time.Second)
	limiter.windows["address-0"] = loginWindow{count: 10, expires: start.Add(-time.Nanosecond)}
	if !loginLimiterAllowAt(limiter, "address-0", start) {
		t.Fatal("maintenance delay prevented an existing expired address from recovering")
	}
	if window := limiter.windows["address-0"]; window.count != 1 || !window.expires.Equal(start.Add(time.Minute)) || len(limiter.windows) != 20000 {
		t.Fatalf("renewing an existing address changed capacity or its window: %+v", window)
	}
}

func TestLoginLimiterConcurrentLimitAndCapacity(t *testing.T) {
	for _, full := range []bool{false, true} {
		t.Run(fmt.Sprintf("full=%t", full), func(t *testing.T) {
			limiter := newLoginLimiter()
			now := time.Unix(1000, 0)
			wantAllowed, wantSize := int32(10), 1
			if full {
				loginLimiterFill(limiter, 19999, now.Add(time.Minute))
				wantAllowed, wantSize = 1, 20000
			}
			var allowed atomic.Int32
			var workers sync.WaitGroup
			start := make(chan struct{})
			for index := 0; index < 128; index++ {
				workers.Add(1)
				go func() {
					defer workers.Done()
					key := "same-client"
					if full {
						key = fmt.Sprintf("new-client-%d", index)
					}
					<-start
					if loginLimiterAllowAt(limiter, key, now) {
						allowed.Add(1)
					}
				}()
			}
			close(start)
			workers.Wait()
			if allowed.Load() != wantAllowed || len(limiter.windows) != wantSize {
				t.Fatalf("concurrent admission = %d allowed, %d addresses; want %d, %d", allowed.Load(), len(limiter.windows), wantAllowed, wantSize)
			}
		})
	}
}

func BenchmarkLoginLimiterHighCardinalityDenials(b *testing.B) {
	for _, count := range []int{10001, 20000} {
		b.Run(fmt.Sprintf("addresses=%d", count), func(b *testing.B) {
			limiter := newLoginLimiter()
			now := time.Unix(1000, 0)
			loginLimiterFill(limiter, count, now.Add(time.Minute))
			loginLimiterAllowAt(limiter, "address-0", now)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if loginLimiterAllowAt(limiter, "address-0", now) {
					b.Fatal("a saturated address was admitted")
				}
			}
		})
	}
}
