//go:build linux

package server

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/activity"
	"github.com/moooyo/goby/internal/notifications"
)

func adminDestinationFixtureInput(endpoint string) map[string]any {
	return map[string]any{"Revision": "0", "Name": "Administrator notices", "Transport": "webhook", "Enabled": true,
		"EventIds": []string{"UserChanges", "BackupFinished"}, "AllowedNetworks": []string{"127.0.0.1/32"}, "Endpoint": endpoint,
		"SMTP":   map[string]any{"Host": "", "Port": 587, "Security": "starttls", "Username": "", "From": "", "To": []string{}},
		"Secret": "administrator-destination-secret-sentinel"}
}

func TestAdminNotificationDestinationsRequireNativeAuthorityAndKeepSecretsPrivate(t *testing.T) {
	f, cookie, csrf, embyHeaders, _ := notificationHTTPFixture(t)
	headers := http.Header{"Origin": {f.cfg.PublicURL}, "X-CSRF-Token": {csrf}}
	input := adminDestinationFixtureInput("https://receiver.example/events")
	expectStatus(t, f.request(t, http.MethodGet, "/admin/v1/notification-destinations", nil, embyHeaders), http.StatusUnauthorized)
	expectStatus(t, f.request(t, http.MethodPost, "/admin/v1/notification-destinations", input, nil, cookie), http.StatusForbidden)
	created := f.request(t, http.MethodPost, "/admin/v1/notification-destinations", input, headers, cookie)
	expectStatus(t, created, http.StatusOK)
	value := jsonObject(t, created)
	id := value["Id"].(string)
	if value["Revision"] != "1" || value["HasSecret"] != true || value["Transport"] != "webhook" {
		t.Fatal("destination lost its canonical revision or safe secret flag")
	}
	for _, response := range []string{created.Body.String(), f.request(t, http.MethodGet, "/admin/v1/notification-destinations", nil, nil, cookie).Body.String()} {
		if strings.Contains(response, "secret-sentinel") || strings.Contains(response, "Ciphertext") || strings.Contains(response, "\"Secret\"") {
			t.Fatal("destination credential escaped a native response")
		}
	}
	var stored string
	if err := f.pool.QueryRow(f.ctx, `SELECT row_to_json(d)::text FROM admin_notification_destinations d WHERE id=$1`, id).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, "secret-sentinel") {
		t.Fatal("destination persisted a plaintext secret")
	}
	input["Revision"] = "0"
	delete(input, "Secret")
	expectStatus(t, f.request(t, http.MethodPut, "/admin/v1/notification-destinations/"+id, input, headers, cookie), http.StatusBadRequest)
	input["Revision"] = "2"
	expectStatus(t, f.request(t, http.MethodPut, "/admin/v1/notification-destinations/"+id, input, headers, cookie), http.StatusConflict)
	input["Revision"] = "1"
	input["Endpoint"] = "https://another-receiver.example/events"
	expectStatus(t, f.request(t, http.MethodPut, "/admin/v1/notification-destinations/"+id, input, headers, cookie), http.StatusBadRequest)
	input["Endpoint"] = "https://receiver.example/events"
	input["Enabled"] = false
	updated := f.request(t, http.MethodPut, "/admin/v1/notification-destinations/"+id, input, headers, cookie)
	expectStatus(t, updated, http.StatusOK)
	if jsonObject(t, updated)["Revision"] != "2" {
		t.Fatal("destination edit did not advance its string revision")
	}
	expectStatus(t, f.request(t, http.MethodPost, "/admin/v1/notification-destinations/"+id+"/test", map[string]any{"Revision": "2"}, headers, cookie), http.StatusBadRequest)
	expectStatus(t, f.request(t, http.MethodDelete, "/admin/v1/notification-destinations/"+id, map[string]any{"Revision": "1"}, headers, cookie), http.StatusConflict)
	expectStatus(t, f.request(t, http.MethodDelete, "/admin/v1/notification-destinations/"+id, map[string]any{"Revision": "2"}, headers, cookie), http.StatusNoContent)
}

func TestAdminNotificationQueueUsesCommittedActionsAndDisablingCancelsWithoutReplay(t *testing.T) {
	f, cookie, csrf, _, _ := notificationHTTPFixture(t)
	headers := http.Header{"Origin": {f.cfg.PublicURL}, "X-CSRF-Token": {csrf}}
	input := adminDestinationFixtureInput("https://receiver.example/events")
	created := f.request(t, http.MethodPost, "/admin/v1/notification-destinations", input, headers, cookie)
	expectStatus(t, created, http.StatusOK)
	id := jsonObject(t, created)["Id"].(string)
	actor, err := f.users.Resolve(f.ctx, cookie.Value, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.users.CreateManagedUser(f.ctx, actor, "Administrator notification event", "notification-event-password", false); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM admin_notification_deliveries WHERE destination_id=$1 AND event_kind='UserChanges' AND state='pending'`, id).Scan(&count); err != nil || count != 1 {
		t.Fatal("committed user creation did not enqueue exactly one subscribed notification")
	}
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = activity.RecordBackupPublished(f.ctx, tx, strings.Repeat("a", 32), strings.Repeat("b", 64), 1024); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM admin_notification_deliveries WHERE destination_id=$1`, id).Scan(&count); err != nil || count != 1 {
		t.Fatal("a rolled-back source leaked a queued notification")
	}
	publication, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = activity.Record(f.ctx, publication, activity.Event{Action: activity.ActionBackupFinished, Severity: activity.SeverityInfo, Source: activity.SourceSystem,
		Actor: activity.Actor{Kind: activity.ActorSystem}, Resource: activity.Resource{Kind: activity.ResourceBackup, ID: "publication-authorized-backup"}, State: activity.StateCompleted}); err != nil {
		t.Fatal(err)
	}
	if err = publication.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM admin_notification_deliveries WHERE destination_id=$1`, id).Scan(&count); err != nil || count != 1 {
		t.Fatal("the pre-publication authorization emitted a completed-backup notification")
	}
	publication, err = f.pool.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err = activity.RecordBackupPublished(f.ctx, publication, strings.Repeat("c", 32), strings.Repeat("d", 64), 2048); err != nil {
			t.Fatal(err)
		}
	}
	if err = publication.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM admin_notification_deliveries WHERE destination_id=$1 AND event_kind='BackupFinished' AND state='pending'`, id).Scan(&count); err != nil || count != 1 {
		t.Fatal("committed publication did not emit exactly one completed-backup notification")
	}
	input["Revision"] = "1"
	input["Enabled"] = false
	delete(input, "Secret")
	expectStatus(t, f.request(t, http.MethodPut, "/admin/v1/notification-destinations/"+id, input, headers, cookie), http.StatusOK)
	if _, err := f.users.CreateManagedUser(f.ctx, actor, "Disabled destination event", "notification-event-password", false); err != nil {
		t.Fatal(err)
	}
	input["Revision"] = "2"
	input["Enabled"] = true
	expectStatus(t, f.request(t, http.MethodPut, "/admin/v1/notification-destinations/"+id, input, headers, cookie), http.StatusOK)
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM admin_notification_deliveries WHERE destination_id=$1 AND state='pending'`, id).Scan(&count); err != nil || count != 0 {
		t.Fatal("re-enabling replayed a cancelled or disabled-period event")
	}
	if _, err := f.users.CreateManagedUser(f.ctx, actor, "Re-enabled destination event", "notification-event-password", false); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM admin_notification_deliveries WHERE destination_id=$1 AND destination_revision=3 AND state='pending'`, id).Scan(&count); err != nil || count != 1 {
		t.Fatal("new generation did not receive its own fresh source event")
	}
}

func TestAdminNotificationWebhookTestIsDeliveredByTheBackgroundRuntime(t *testing.T) {
	f, cookie, csrf, _, _ := notificationHTTPFixture(t)
	headers := http.Header{"Origin": {f.cfg.PublicURL}, "X-CSRF-Token": {csrf}}
	received := make(chan notifications.AdminMessage, 1)
	receiver := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var message notifications.AdminMessage
		raw, err := io.ReadAll(io.LimitReader(r.Body, 4097))
		if err != nil || len(raw) > 4096 || json.Unmarshal(raw, &message) != nil || r.Header.Get("Idempotency-Key") != message.EventID || r.Header.Get("X-Goby-Signature") != notifications.Signature("administrator-destination-secret-sentinel", r.Header.Get("X-Goby-Timestamp"), raw) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		received <- message
		w.WriteHeader(http.StatusNoContent)
	}))
	defer receiver.Close()
	created := f.request(t, http.MethodPost, "/admin/v1/notification-destinations", adminDestinationFixtureInput(receiver.URL), headers, cookie)
	expectStatus(t, created, http.StatusOK)
	id := jsonObject(t, created)["Id"].(string)
	queued := f.request(t, http.MethodPost, "/admin/v1/notification-destinations/"+id+"/test", map[string]any{"Revision": "1"}, headers, cookie)
	expectStatus(t, queued, http.StatusAccepted)
	queuedID := jsonObject(t, queued)["Id"].(string)
	roots := x509.NewCertPool()
	roots.AddCert(receiver.Certificate())
	f.app.notificationRuntime = notifications.NewRuntime(f.app.notificationStore, notifications.RuntimeOptions{RootCAs: roots})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := f.app.notificationRuntime.Close(ctx); err != nil {
			t.Error(err)
		}
	}()
	select {
	case message := <-received:
		if message.EventID != queuedID || message.Kind != "Test" || message.Title != "Goby test notification" {
			t.Fatal("background delivery lost its admitted identity")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("queued administrator test was not delivered")
	}
	waitNotificationCondition(t, f, "acknowledged administrator test did not retain its result", func() bool {
		var delivered bool
		err := f.pool.QueryRow(f.ctx, `SELECT EXISTS(SELECT 1 FROM admin_notification_deliveries q JOIN admin_notification_destinations d ON d.id=q.destination_id WHERE q.id=$1 AND q.state='delivered' AND q.outcome='delivered' AND d.last_outcome='delivered' AND d.last_delivered_at IS NOT NULL)`, queuedID).Scan(&delivered)
		return err == nil && delivered
	})
}
