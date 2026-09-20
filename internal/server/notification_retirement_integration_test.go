//go:build linux

package server

import (
	"context"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/notifications"
)

// The outbound request uses real TLS with an explicitly trusted private root.
// The revoke enters the complete authenticated HTTP handler. No delivery or
// revocation method is stubbed and no global certificate check is disabled.
func TestHTTPNotificationLogoutCancelsAndJoinsAnActualTrustedTLSAttempt(t *testing.T) {
	f, cookie, csrf, headers, _ := notificationHTTPFixture(t)
	started, ended := make(chan struct{}), make(chan struct{})
	var first, closed sync.Once
	var requests atomic.Int32
	receiver := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		raw, err := io.ReadAll(io.LimitReader(r.Body, 32769))
		if err != nil || len(raw) > 32768 || r.Header.Get("X-Goby-Target-Token") != "target-secret-sentinel-48" || r.Header.Get("X-Goby-Signature") != notifications.Signature("receiver-secret-sentinel-48", r.Header.Get("X-Goby-Timestamp"), raw) {
			http.Error(w, "Invalid request", 401)
			return
		}
		first.Do(func() { close(started) })
		<-r.Context().Done()
		closed.Do(func() { close(ended) })
	}))
	defer receiver.Close()
	roots := x509.NewCertPool()
	roots.AddCert(receiver.Certificate())
	f.app.notificationRuntime = notifications.NewRuntime(f.app.notificationStore, notifications.RuntimeOptions{RootCAs: roots})
	defer func() {
		ctx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if err := f.app.notificationRuntime.Close(ctx); err != nil {
			t.Error("notification worker did not close")
		}
	}()
	expectStatus(t, f.request(t, http.MethodPut, "/admin/v1/notifications", map[string]any{"Revision": "1", "Enabled": true, "Endpoint": receiver.URL + "/events", "AllowedNetworks": []string{"127.0.0.1/32"}, "ReceiverCredential": "receiver-secret-sentinel-48"}, http.Header{"X-CSRF-Token": {csrf}, "Origin": {f.cfg.PublicURL}}, cookie), 200)
	registered := registerNotificationFixture(t, f, headers)
	actor, err := f.users.Resolve(f.ctx, headers.Get("X-Emby-Token"), "emby")
	if err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Notifications/Test", nil, headers), http.StatusAccepted)
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("actual authenticated TLS attempt did not start")
	}
	if f.app.notificationRuntime.ActiveForSession(actor.SessionID) != 1 {
		t.Fatal("outbound attempt was not owned by its login")
	}
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Logout", nil, headers), http.StatusNoContent)
	if f.app.notificationRuntime.ActiveForSession(actor.SessionID) != 0 {
		t.Fatal("logout reported success before cancel/join completed")
	}
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("controlled receiver did not observe outbound cancellation")
	}
	var pending int
	if err = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM notification_deliveries WHERE registration_id=$1 AND state='sending'`, registered["Id"]).Scan(&pending); err != nil || pending != 0 {
		t.Fatal("logout left an active delivery lease")
	}
	select {
	case <-time.After(1500 * time.Millisecond):
	case <-f.ctx.Done():
		t.Fatal("fixture ended during no-redelivery observation")
	}
	if requests.Load() != 1 {
		t.Fatal("revoked login admitted another external request")
	}
}
