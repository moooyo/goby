//go:build linux

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func TestHTTPActivityReadDoesNotWaitForCatalogWriter(t *testing.T) {
	f, cookie, _, _ := adminSettingsHTTPFixture(t)
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	go func() {
		finished <- f.app.library.WithOwnedTx(f.ctx, func(tx library.OwnedTx) error {
			close(entered)
			<-release
			var one int
			return tx.QueryRow("SELECT 1").Scan(&one)
		})
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("catalog writer did not enter its transaction")
	}
	ctx, cancel := context.WithTimeout(f.ctx, 2*time.Second)
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/admin/v1/activity", nil).WithContext(ctx)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		f.handler.ServeHTTP(response, request)
	}()
	select {
	case <-readDone:
	case <-ctx.Done():
		unblock()
		<-finished
		<-readDone
		t.Fatal("activity read waited for the catalog writer")
	}
	if ctx.Err() != nil || response.Code != http.StatusOK {
		t.Fatalf("activity read waited for the catalog writer: status=%d error=%v", response.Code, ctx.Err())
	}
	unblock()
	if err := <-finished; err != nil {
		t.Fatalf("catalog writer failed after concurrent activity read: %v", err)
	}
}

func TestDiagnosticAuthorizationDoesNotWaitForActorRowLocks(t *testing.T) {
	f, cookie, _, _ := adminSettingsHTTPFixture(t)
	principal, err := f.users.Resolve(f.ctx, cookie.Value, "admin")
	if err != nil {
		t.Fatal(err)
	}
	blocker, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	var id string
	if err := blocker.QueryRow(f.ctx, "SELECT id FROM users WHERE id=$1 FOR UPDATE", principal.User.ID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(f.ctx, time.Second)
	defer cancel()
	if err := f.app.checkObservabilityAdministrator(ctx, principal, identity.AdministratorNative); err != nil {
		t.Fatalf("diagnostic authorization waited for an actor lock: %v", err)
	}
}

func TestHTTPActivityCancellationReleasesAuthorizationLocks(t *testing.T) {
	f, cookie, _, _ := adminSettingsHTTPFixture(t)
	principal, err := f.users.Resolve(f.ctx, cookie.Value, "admin")
	if err != nil {
		t.Fatal(err)
	}
	blocker, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(context.Background())
	if _, err := blocker.Exec(f.ctx, "LOCK TABLE activity_entries IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/admin/v1/activity", nil).WithContext(ctx)
	request.AddCookie(cookie)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		f.handler.ServeHTTP(httptest.NewRecorder(), request)
	}()
	wait, stop := context.WithTimeout(f.ctx, 5*time.Second)
	defer stop()
	for {
		var waiting bool
		if err := f.pool.QueryRow(wait, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity activity
			JOIN pg_locks locks ON locks.pid=activity.pid WHERE locks.relation='activity_entries'::regclass
			AND activity.wait_event_type='Lock' AND activity.query LIKE 'WITH filtered AS (%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-finished:
			t.Fatal("activity request returned before reaching its table barrier")
		case <-wait.Done():
			t.Fatal("activity request did not reach its table barrier")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled activity reader retained its transaction")
	}
	var id string
	if err := blocker.QueryRow(wait, "SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT", principal.User.ID).Scan(&id); err != nil {
		t.Fatalf("cancelled activity reader retained actor locks: %v", err)
	}
}
