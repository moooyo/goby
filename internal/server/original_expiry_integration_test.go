//go:build linux

package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"
)

func blockOriginalSessionRevalidation(t *testing.T, fixture *serverFixture) {
	t.Helper()
	tx, err := fixture.pool.Begin(fixture.ctx)
	if err != nil {
		t.Fatalf("begin isolated session revalidation blocker: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(ctx)
	})
	// The fixture has its own schema. This blocks identity reads without
	// changing the session or allowing a later query to observe its expiry.
	if _, err := tx.Exec(fixture.ctx, "LOCK TABLE sessions IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatalf("block isolated session revalidation: %v", err)
	}
}

func TestOriginalExpiryBoundsBlockedInitialAuthorization(t *testing.T) {
	fixture := newStreamHTTPFixture(t)
	principal, err := fixture.f.users.ResolveEmby(fixture.f.ctx, fixture.token)
	if err != nil {
		t.Fatal(err)
	}
	file, source, err := fixture.f.app.library.OpenMedia(fixture.f.ctx, fixture.viewerID, fixture.video.id, "")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if err := fixture.f.pool.QueryRow(fixture.f.ctx, `UPDATE sessions SET
		created_at = clock_timestamp() - interval '1 minute',
		expires_at = clock_timestamp() + interval '2 seconds'
		WHERE id = $1 RETURNING expires_at`, principal.SessionID).Scan(&principal.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	blockOriginalSessionRevalidation(t, fixture.f)
	if !time.Now().Before(principal.ExpiresAt) {
		t.Fatal("session expired before its initial revalidation was blocked")
	}
	request := httptest.NewRequest(http.MethodGet, "/emby/Videos/"+fixture.video.id+"/original.mp4", nil)
	request = request.WithContext(context.WithValue(fixture.f.ctx, principalKey, principal))
	result := make(chan error, 1)
	go func() {
		_, finish, err := fixture.f.app.guardOriginalMedia(httptest.NewRecorder(), request, file, source)
		if finish != nil {
			finish()
		}
		result <- err
	}()
	deadline := time.NewTimer(time.Until(principal.ExpiresAt) + 2*time.Second)
	defer deadline.Stop()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expired initial authorization returned %v, want cancellation", err)
		}
	case <-deadline.C:
		t.Fatal("known credential expiry did not interrupt blocked initial authorization")
	}
	fixture.f.app.originals.mu.Lock()
	owners := len(fixture.f.app.originals.owners)
	fixture.f.app.originals.mu.Unlock()
	if owners != 0 {
		t.Fatalf("expired initial authorization retained %d original owners", owners)
	}
}

func TestHTTPOriginalExpiryInterruptsBlockedResponseDuringIdentityTimeout(t *testing.T) {
	fixture := newStreamHTTPFixture(t)
	target := originalRevocationSparseSource(t, fixture)
	principal, err := fixture.f.users.ResolveEmby(fixture.f.ctx, fixture.token)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.f.pool.QueryRow(fixture.f.ctx, `UPDATE sessions SET
		created_at = clock_timestamp() - interval '1 minute',
		expires_at = clock_timestamp() + interval '7 seconds'
		WHERE id = $1 RETURNING expires_at`, principal.SessionID).Scan(&principal.ExpiresAt); err != nil {
		t.Fatal(err)
	}
	opened := originalRevocationOpen(t, fixture, target, fixture.token)
	blockOriginalSessionRevalidation(t, fixture.f)
	if time.Until(principal.ExpiresAt) <= 6*time.Second {
		t.Fatal("session expiry left no complete watcher interval under blocked identity reads")
	}
	// The first watcher query times out before expiry. A later database result
	// cannot revoke this response while the exclusive table lock remains held.
	// The credential deadline must interrupt its blocked write independently.
	originalRevocationWaitSlots(t, fixture, 0)
	if time.Now().Before(principal.ExpiresAt) {
		t.Fatal("a transient identity timeout cancelled an unexpired original grant")
	}
	originalRevocationAssertAborted(t, opened)
	fixture.f.app.originals.mu.Lock()
	owners := len(fixture.f.app.originals.owners)
	fixture.f.app.originals.mu.Unlock()
	if owners != 0 {
		t.Fatalf("expired original response retained %d original owners", owners)
	}
	policy := fixture.f.app.playbackPolicyGate()
	policy.mu.Lock()
	leases := len(policy.leases)
	policy.mu.Unlock()
	if leases != 0 {
		t.Fatalf("expired original response retained %d playback leases", leases)
	}
}

func TestOriginalExpiryUsesEarliestAuthenticatedDeadline(t *testing.T) {
	for _, test := range []struct {
		name             string
		databaseLifetime time.Duration
		snapshotOffset   time.Duration
		missingSnapshot  bool
	}{
		{name: "fresh shorter", databaseLifetime: 3 * time.Second, snapshotOffset: time.Minute},
		{name: "missing snapshot", databaseLifetime: 3 * time.Second, missingSnapshot: true},
		{name: "initial shorter", databaseLifetime: 8 * time.Second, snapshotOffset: -6 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newStreamHTTPFixture(t)
			principal, err := fixture.f.users.ResolveEmby(fixture.f.ctx, fixture.token)
			if err != nil {
				t.Fatal(err)
			}
			file, source, err := fixture.f.app.library.OpenMedia(fixture.f.ctx, fixture.viewerID, fixture.video.id, "")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			var currentExpiry time.Time
			if err := fixture.f.pool.QueryRow(fixture.f.ctx, `UPDATE sessions SET
				created_at = clock_timestamp() - interval '1 minute',
				expires_at = clock_timestamp() + $2::bigint * interval '1 second'
				WHERE id = $1 RETURNING expires_at`, principal.SessionID, int64(test.databaseLifetime/time.Second)).Scan(&currentExpiry); err != nil {
				t.Fatal(err)
			}
			principal.ExpiresAt = currentExpiry.Add(test.snapshotOffset)
			if test.missingSnapshot {
				principal.ExpiresAt = time.Time{}
			}
			expectedExpiry := currentExpiry
			if !principal.ExpiresAt.IsZero() && principal.ExpiresAt.Before(expectedExpiry) {
				expectedExpiry = principal.ExpiresAt
			}
			request := httptest.NewRequest(http.MethodGet, "/emby/Videos/"+fixture.video.id+"/original.mp4", nil)
			request = request.WithContext(context.WithValue(fixture.f.ctx, principalKey, principal))
			work, finish, err := fixture.f.app.guardOriginalMedia(httptest.NewRecorder(), request, file, source)
			if err != nil {
				t.Fatal(err)
			}
			finishGuard := sync.OnceFunc(finish)
			t.Cleanup(finishGuard)
			blockOriginalSessionRevalidation(t, fixture.f)
			if !time.Now().Before(expectedExpiry) {
				t.Fatal("earliest credential deadline elapsed before identity reads were blocked")
			}
			deadline := time.NewTimer(time.Until(expectedExpiry) + 2*time.Second)
			defer deadline.Stop()
			select {
			case <-work.Done():
				if time.Now().Before(expectedExpiry) {
					t.Fatal("original response ended before its earliest credential deadline")
				}
			case <-deadline.C:
				t.Fatal("original response retained a stale or extended credential deadline")
			}
			// Context cancellation precedes descriptor closure. Let the watcher
			// close it before finish can select the normal completion branch.
			closedBy := time.NewTimer(2 * time.Second)
			defer closedBy.Stop()
			ticker := time.NewTicker(10 * time.Millisecond)
			defer ticker.Stop()
			for {
				if _, err := file.Stat(); errors.Is(err, os.ErrClosed) {
					break
				}
				select {
				case <-closedBy.C:
					t.Fatal("expired original response retained its source descriptor")
				case <-ticker.C:
				}
			}
			finishGuard()
			fixture.f.app.originals.mu.Lock()
			owners := len(fixture.f.app.originals.owners)
			fixture.f.app.originals.mu.Unlock()
			policy := fixture.f.app.playbackPolicyGate()
			policy.mu.Lock()
			leases := len(policy.leases)
			policy.mu.Unlock()
			if owners != 0 || leases != 0 {
				t.Fatalf("effective expiry retained original resources: owners = %d, leases = %d", owners, leases)
			}
		})
	}
}
