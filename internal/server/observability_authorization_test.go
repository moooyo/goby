//go:build linux

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/identity"
)

func TestHTTPActivityRejectsAnAdministratorExpiringWhileTheQueryWaits(t *testing.T) {
	f, cookie, _, _ := adminSettingsHTTPFixture(t)
	principal, err := f.users.Resolve(f.ctx, cookie.Value, "admin")
	if err != nil {
		t.Fatal("resolve the owned activity administrator")
	}
	blocker, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal("start the owned activity read barrier")
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = blocker.Rollback(ctx)
	})
	if _, err := blocker.Exec(f.ctx, "LOCK TABLE activity_entries IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal("hold the owned activity table read barrier")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE sessions SET created_at = clock_timestamp() - interval '1 hour',
		expires_at = clock_timestamp() + interval '2 seconds' WHERE id = $1`, principal.SessionID); err != nil {
		t.Fatal("prepare the owned activity administrator's natural expiry")
	}
	request := httptest.NewRequest(http.MethodGet, "/admin/v1/activity", nil).WithContext(f.ctx)
	request.AddCookie(cookie)
	finished := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response := httptest.NewRecorder()
		f.handler.ServeHTTP(response, request)
		finished <- response
	}()
	wait, cancel := context.WithTimeout(f.ctx, 8*time.Second)
	defer cancel()
	for {
		var waiting bool
		if err := f.pool.QueryRow(wait, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity activity
			JOIN pg_locks locks ON locks.pid = activity.pid WHERE locks.relation = 'activity_entries'::regclass
			AND activity.wait_event_type = 'Lock' AND activity.query LIKE 'WITH filtered AS (%')`).Scan(&waiting); err != nil {
			t.Fatal("observe the owned activity query wait")
		}
		if waiting {
			break
		}
		select {
		case <-finished:
			t.Fatal("the activity request did not reach its query before expiry")
		case <-wait.Done():
			t.Fatal("the activity query never reached its owned barrier")
		case <-time.After(10 * time.Millisecond):
		}
	}
	for {
		var expired bool
		if err := f.pool.QueryRow(wait, "SELECT expires_at <= clock_timestamp() FROM sessions WHERE id = $1", principal.SessionID).Scan(&expired); err != nil {
			t.Fatal("observe the owned activity administrator expiry")
		}
		if expired {
			break
		}
		select {
		case <-wait.Done():
			t.Fatal("the owned activity administrator did not expire")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := blocker.Rollback(wait); err != nil {
		t.Fatal("release the owned activity table barrier")
	}
	select {
	case response := <-finished:
		nativeObservabilityError(t, response, http.StatusUnauthorized, "invalid_credentials")
	case <-wait.Done():
		t.Fatal("the expired activity query did not complete after its barrier was released")
	}
}

func TestNativeObservabilityRevalidatesAnApplicationPrincipalBeforeParsing(t *testing.T) {
	f := newApplicationKeyHTTPFixture(t)
	key := f.create(t, "Activity audience fixture")
	principal, err := f.users.ResolveEmbyForClient(f.ctx, key.token, identity.Client{Name: "Activity audience fixture", DeviceID: "activity-native-boundary"})
	if err != nil || !principal.IsApplicationKey() {
		t.Fatal("resolve the owned complete application principal")
	}
	for _, route := range []struct {
		path    string
		handler http.HandlerFunc
	}{
		{"/admin/v1/activity?Limit=invalid", f.app.adminActivity},
		{"/admin/v1/logs?Limit=invalid", f.app.adminDiagnosticLogs},
		{"/admin/v1/logs/missing.jsonl/lines?Limit=invalid", f.app.adminDiagnosticLines},
		{"/admin/v1/logs/missing.jsonl/download?Sanitize=false", f.app.adminDiagnosticDownload},
	} {
		request := httptest.NewRequest(http.MethodGet, route.path, nil).WithContext(context.WithValue(f.ctx, principalKey, principal))
		response := httptest.NewRecorder()
		observabilityNoCache(route.handler)(response, request)
		nativeObservabilityError(t, response, http.StatusUnauthorized, "invalid_credentials")
	}
}
