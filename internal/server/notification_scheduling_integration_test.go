//go:build linux

package server

import (
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/notificationjournal"
	"github.com/moooyo/goby/internal/notifications"
)

func additionalNotificationRegistration(t *testing.T, f *serverFixture, name string) (http.Header, string, string) {
	t.Helper()
	user, err := f.users.CreateUser(f.ctx, name, "notification-viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	login := f.embyLogin(t, user.Name, "notification-viewer-password")
	headers := http.Header{"X-Emby-Token": {stringValue(t, login, "AccessToken")}}
	registration := registerNotificationFixture(t, f, headers)["Id"].(string)
	actor, err := f.users.Resolve(f.ctx, headers.Get("X-Emby-Token"), "emby")
	if err != nil {
		t.Fatal(err)
	}
	return headers, registration, actor.SessionID
}

func waitNotificationCondition(t *testing.T, f *serverFixture, description string, check func() bool) {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for !check() {
		select {
		case <-deadline:
			t.Fatal(description)
		case <-f.ctx.Done():
			t.Fatal(f.ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
}

func TestNotificationStalledDestinationDoesNotBlockOtherRegistrationsOrMaintenance(t *testing.T) {
	f, cookie, csrf, headers, _ := notificationHTTPFixture(t)
	var slowID string
	started, ended := make(chan struct{}), make(chan struct{})
	fast := make(chan notifications.Payload, 8)
	var slowRequests atomic.Int32
	receiver := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload notifications.Payload
		if json.NewDecoder(r.Body).Decode(&payload) != nil {
			http.Error(w, "Invalid notification", http.StatusBadRequest)
			return
		}
		if payload.RegistrationID == slowID {
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			if slowRequests.Add(1) == 1 {
				close(started)
				defer close(ended)
			}
			<-r.Context().Done()
			return
		}
		fast <- payload
		_ = json.NewEncoder(w).Encode(map[string]any{"EventId": payload.EventID, "Accepted": true})
	}))
	defer receiver.Close()
	expectStatus(t, f.request(t, http.MethodPut, "/admin/v1/notifications", map[string]any{
		"Revision": "1", "Enabled": true, "Endpoint": receiver.URL,
		"AllowedNetworks": []string{"127.0.0.1/32"}, "ReceiverCredential": "receiver-secret-sentinel-48",
	}, http.Header{"X-CSRF-Token": {csrf}, "Origin": {f.cfg.PublicURL}}, cookie), http.StatusOK)
	slowID = registerNotificationFixture(t, f, headers)["Id"].(string)
	slowActor, err := f.users.Resolve(f.ctx, headers.Get("X-Emby-Token"), "emby")
	if err != nil {
		t.Fatal(err)
	}
	fastHeaders, fastID, _ := additionalNotificationRegistration(t, f, "Fast notification viewer")
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Notifications/Test", nil, headers), http.StatusAccepted)
	roots := x509.NewCertPool()
	roots.AddCert(receiver.Certificate())
	f.app.notificationRuntime = notifications.NewRuntime(f.app.notificationStore, notifications.RuntimeOptions{RootCAs: roots})
	defer closeNotificationFixtureRuntime(t, f)
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("stalled destination did not begin")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE notification_deliveries SET lease_until=clock_timestamp()-interval '1 second' WHERE registration_id=$1 AND state='sending'`, slowID); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Notifications/Test", nil, fastHeaders), http.StatusAccepted)
	if err := notificationjournal.RecordCatalog(f.ctx, f.pool, notificationjournal.NewID(), []notificationjournal.Reference{{Kind: "Item", ID: "missing-item", LibraryID: "missing-library"}}, false); err != nil {
		t.Fatal(err)
	}
	var sequence int64
	if err := f.pool.QueryRow(f.ctx, `SELECT sequence FROM notification_journal_state WHERE id=1`).Scan(&sequence); err != nil {
		t.Fatal(err)
	}
	select {
	case payload := <-fast:
		if payload.RegistrationID != fastID {
			t.Fatalf("unexpected fast registration: %s", payload.RegistrationID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a stalled destination blocked an unrelated registration")
	}
	waitNotificationCondition(t, f, "stalled destination blocked lease recovery, fanout, or source reclamation", func() bool {
		var recovered bool
		if err := f.pool.QueryRow(f.ctx, `SELECT
			EXISTS(SELECT 1 FROM notification_deliveries WHERE registration_id=$1 AND state='pending' AND attempts=1)
			AND (SELECT count(*) FROM notification_registrations WHERE id=ANY($2::text[]) AND source_cursor=$3)=2
			AND NOT EXISTS(SELECT 1 FROM notification_source_events)`, slowID, []string{slowID, fastID}, sequence).Scan(&recovered); err != nil {
			t.Fatal(err)
		}
		return recovered
	})
	if slowRequests.Load() != 1 || f.app.notificationRuntime.ActiveForSession(slowActor.SessionID) != 1 {
		t.Fatal("an expired database lease replaced its still-owned sender")
	}
	closeNotificationFixtureRuntime(t, f)
	if f.app.notificationRuntime.ActiveForSession(slowActor.SessionID) != 0 {
		t.Fatal("Close returned before the stalled sender retired")
	}
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("stalled receiver did not observe Close cancellation")
	}
}

func TestNotificationSendersAreBoundedAndCloseJoinsEveryRegistration(t *testing.T) {
	f, cookie, csrf, headers, _ := notificationHTTPFixture(t)
	started, ended := make(chan string, 8), make(chan string, 8)
	releases := make(map[string]chan struct{})
	receiver := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload notifications.Payload
		if json.NewDecoder(r.Body).Decode(&payload) != nil {
			http.Error(w, "Invalid notification", http.StatusBadRequest)
			return
		}
		started <- payload.RegistrationID
		defer func() { ended <- payload.RegistrationID }()
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-releases[payload.RegistrationID]:
			_ = json.NewEncoder(w).Encode(map[string]any{"EventId": payload.EventID, "Accepted": true})
		case <-r.Context().Done():
		}
	}))
	defer receiver.Close()
	expectStatus(t, f.request(t, http.MethodPut, "/admin/v1/notifications", map[string]any{
		"Revision": "1", "Enabled": true, "Endpoint": receiver.URL,
		"AllowedNetworks": []string{"127.0.0.1/32"}, "ReceiverCredential": "receiver-secret-sentinel-48",
	}, http.Header{"X-CSRF-Token": {csrf}, "Origin": {f.cfg.PublicURL}}, cookie), http.StatusOK)
	firstID := registerNotificationFixture(t, f, headers)["Id"].(string)
	actor, err := f.users.Resolve(f.ctx, headers.Get("X-Emby-Token"), "emby")
	if err != nil {
		t.Fatal(err)
	}
	sessions := []string{actor.SessionID}
	releases[firstID] = make(chan struct{})
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Notifications/Test", nil, headers), http.StatusAccepted)
	for index := 1; index < 5; index++ {
		header, id, session := additionalNotificationRegistration(t, f, fmt.Sprintf("Bounded notification viewer %d", index))
		releases[id] = make(chan struct{})
		sessions = append(sessions, session)
		expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Notifications/Test", nil, header), http.StatusAccepted)
	}
	roots := x509.NewCertPool()
	roots.AddCert(receiver.Certificate())
	f.app.notificationRuntime = notifications.NewRuntime(f.app.notificationStore, notifications.RuntimeOptions{RootCAs: roots})
	defer closeNotificationFixtureRuntime(t, f)
	for range 4 {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("bounded senders did not start independent registrations")
		}
	}
	select {
	case <-started:
		t.Fatal("more than four unretired senders were admitted")
	case <-time.After(100 * time.Millisecond):
	}
	close(releases[firstID])
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("a retired sender did not release capacity")
	}
	closeNotificationFixtureRuntime(t, f)
	for _, session := range sessions {
		if f.app.notificationRuntime.ActiveForSession(session) != 0 {
			t.Fatal("Close returned with an unretired registration")
		}
	}
	for range 5 {
		select {
		case <-ended:
		case <-time.After(3 * time.Second):
			t.Fatal("Close left an actual receiver request active")
		}
	}
}

func TestNotificationHistoryPrunesOnChangeAndOnlyUsesIdleFallbackAtStartup(t *testing.T) {
	f, cookie, csrf, headers, _ := notificationHTTPFixture(t)
	configureNotificationFixture(t, f, cookie, csrf)
	registration := registerNotificationFixture(t, f, headers)["Id"].(string)
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO notification_deliveries(id,registration_id,registration_revision,transport_revision,source_sequence,kind,refs,state,due_at)
		SELECT md5('notification-history-'||n::text),$1,1,2,n,'Test','[]'::jsonb,CASE WHEN n<=40 THEN 'delivered' ELSE 'pending' END,clock_timestamp()+interval '1 hour' FROM generate_series(1,46)n`, registration); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `CREATE TABLE notification_history_prunes(count integer NOT NULL);
		INSERT INTO notification_history_prunes VALUES(0);
		CREATE FUNCTION notification_count_history_prune() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN UPDATE notification_history_prunes SET count=count+1; RETURN NULL; END $$;
		CREATE TRIGGER notification_history_prune AFTER DELETE ON notification_deliveries
		FOR EACH STATEMENT EXECUTE FUNCTION notification_count_history_prune()`); err != nil {
		t.Fatal(err)
	}
	f.app.notificationRuntime = notifications.NewRuntime(f.app.notificationStore)
	defer closeNotificationFixtureRuntime(t, f)
	waitNotificationCondition(t, f, "startup did not prune restored terminal history", func() bool {
		var terminal, pending int
		if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FILTER(WHERE state NOT IN('pending','sending')),count(*) FILTER(WHERE state='pending') FROM notification_deliveries WHERE registration_id=$1`, registration).Scan(&terminal, &pending); err != nil {
			t.Fatal(err)
		}
		return terminal == 32 && pending == 6
	})
	select {
	case <-time.After(1200 * time.Millisecond):
	case <-f.ctx.Done():
		t.Fatal(f.ctx.Err())
	}
	var prunes int
	if err := f.pool.QueryRow(f.ctx, `SELECT count FROM notification_history_prunes`).Scan(&prunes); err != nil || prunes != 1 {
		t.Fatalf("unchanged history still ran retention SQL: count=%d error=%v", prunes, err)
	}
	expectStatus(t, f.request(t, http.MethodPut, "/emby/Sessions/Notifications", map[string]any{
		"Revision": "1", "Transport": "GobyWebhookV1", "EventIds": []string{"CatalogInvalidated"},
	}, headers), http.StatusOK)
	var terminal, pending int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FILTER(WHERE state NOT IN('pending','sending')),count(*) FILTER(WHERE state IN('pending','sending')) FROM notification_deliveries WHERE registration_id=$1`, registration).Scan(&terminal, &pending); err != nil || terminal != 32 || pending != 0 {
		t.Fatalf("registration mutation did not immediately bound terminal history: terminal=%d pending=%d error=%v", terminal, pending, err)
	}
	_, oldRegistration, _ := additionalNotificationRegistration(t, f, "Expired notification history viewer")
	if _, err := f.pool.Exec(f.ctx, `UPDATE notification_registrations SET enabled=false,updated_at=clock_timestamp()-interval '8 days' WHERE id=$1`, oldRegistration); err != nil {
		t.Fatal(err)
	}
	select {
	case <-time.After(1100 * time.Millisecond):
	case <-f.ctx.Done():
		t.Fatal(f.ctx.Err())
	}
	var exists bool
	if err := f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM notification_registrations WHERE id=$1)`, oldRegistration).Scan(&exists); err != nil || !exists {
		t.Fatalf("seven-day cleanup still ran on the fast path: exists=%v error=%v", exists, err)
	}
	closeNotificationFixtureRuntime(t, f)
	f.app.notificationRuntime = notifications.NewRuntime(f.app.notificationStore)
	waitNotificationCondition(t, f, "restart fallback did not remove expired registration history", func() bool {
		if err := f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM notification_registrations WHERE id=$1)`, oldRegistration).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		return !exists
	})
}
