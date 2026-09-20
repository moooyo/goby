//go:build linux

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/notificationjournal"
	"github.com/moooyo/goby/internal/notifications"
)

func notificationHTTPFixture(t *testing.T) (*serverFixture, *http.Cookie, string, http.Header, string) {
	t.Helper()
	f := newServerFixture(t)
	f.bootstrap(t)
	if err := f.app.notificationRuntime.Close(f.ctx); err != nil {
		t.Fatal(err)
	}
	f.users = identity.NewWithApplicationKeyVault(f.pool, identity.NewApplicationKeyVault(filepath.Join(t.TempDir(), "notification-master.key")))
	f.app.identity = f.users
	f.app.notificationStore = notifications.NewStore(f.pool, f.users, f.app.library)
	cookie, csrf := f.adminLogin(t)
	viewer, err := f.users.CreateUser(f.ctx, "Notification viewer", "notification-viewer-password", false)
	if err != nil {
		t.Fatal(err)
	}
	login := f.embyLogin(t, viewer.Name, "notification-viewer-password")
	headers := http.Header{"X-Emby-Token": {stringValue(t, login, "AccessToken")}}
	return f, cookie, csrf, headers, viewer.ID
}
func configureNotificationFixture(t *testing.T, f *serverFixture, cookie *http.Cookie, csrf string) {
	t.Helper()
	response := f.request(t, http.MethodPut, "/admin/v1/notifications", map[string]any{"Revision": "1", "Enabled": true, "Endpoint": "https://receiver.invalid/events", "AllowedNetworks": []string{}, "ReceiverCredential": "receiver-secret-sentinel-48"}, http.Header{"X-CSRF-Token": {csrf}, "Origin": {f.cfg.PublicURL}}, cookie)
	expectStatus(t, response, 200)
}
func registerNotificationFixture(t *testing.T, f *serverFixture, headers http.Header) map[string]any {
	t.Helper()
	response := f.request(t, http.MethodPut, "/emby/Sessions/Notifications", map[string]any{"Revision": "0", "Transport": "GobyWebhookV1", "TargetToken": "target-secret-sentinel-48", "EventIds": []string{"CatalogInvalidated", "UserDataInvalidated"}}, headers)
	expectStatus(t, response, 200)
	return jsonObject(t, response)
}

func TestHTTPNotificationsRegistrationIsSeparateSecretSafeAndGenerationBound(t *testing.T) {
	f, cookie, csrf, headers, _ := notificationHTTPFixture(t)
	configureNotificationFixture(t, f, cookie, csrf)
	registered := registerNotificationFixture(t, f, headers)
	if registered["Revision"] != "1" || registered["Transport"] != "GobyWebhookV1" || registered["Enabled"] != true || registered["HasTargetToken"] != true {
		t.Fatal("registration lost its safe state or generation")
	}
	for _, path := range []string{"/emby/Sessions/Notifications", "/emby/Sessions"} {
		response := f.request(t, http.MethodGet, path, nil, headers)
		expectStatus(t, response, 200)
		if strings.Contains(response.Body.String(), "secret-sentinel") || strings.Contains(response.Body.String(), "ciphertext") {
			t.Fatal("notification secrets escaped a client DTO")
		}
	}
	expectStatus(t, f.request(t, http.MethodPut, "/emby/Sessions/Notifications", map[string]any{"Revision": "0", "Transport": "GobyWebhookV1", "EventIds": []string{"CatalogInvalidated"}}, headers), 409)
	rotated := f.request(t, http.MethodPut, "/emby/Sessions/Notifications", map[string]any{"Revision": "1", "Transport": "GobyWebhookV1", "TargetToken": "rotated-target-sentinel-48", "EventIds": []string{"CatalogInvalidated"}}, headers)
	expectStatus(t, rotated, 200)
	value := jsonObject(t, rotated)
	if value["Id"] != registered["Id"] || value["Revision"] != "2" {
		t.Fatal("rotation changed identity or lost CAS")
	}
	expectStatus(t, f.request(t, http.MethodDelete, "/emby/Sessions/Notifications?Revision=1", nil, headers), 409)
	deleted := f.request(t, http.MethodDelete, "/emby/Sessions/Notifications?Revision=2", nil, headers)
	expectStatus(t, deleted, 200)
	value = jsonObject(t, deleted)
	if value["Enabled"] != false || value["LastOutcome"] != "revoked" || value["Revision"] != "3" {
		t.Fatal("revocation did not advance its generation")
	}
	var config, registrations string
	if f.pool.QueryRow(f.ctx, `SELECT row_to_json(c)::text FROM notification_transport c`).Scan(&config) != nil || f.pool.QueryRow(f.ctx, `SELECT row_to_json(r)::text FROM notification_registrations r`).Scan(&registrations) != nil {
		t.Fatal("read sealed notification fixtures")
	}
	if strings.Contains(config, "receiver-secret") || strings.Contains(registrations, "target-sentinel") {
		t.Fatal("plaintext notification token entered database state")
	}
}

func TestHTTPNotificationsDoNotRegisterVendorCapabilitiesOrApplicationKeys(t *testing.T) {
	f, cookie, csrf, headers, _ := notificationHTTPFixture(t)
	configureNotificationFixture(t, f, cookie, csrf)
	expectStatus(t, f.request(t, http.MethodPost, "/emby/Sessions/Capabilities/Full", map[string]any{"SupportsMediaControl": true, "PushToken": "firebase-secret-sentinel", "PushTokenType": "Firebase"}, headers), 204)
	var count int
	var capabilities string
	if f.pool.QueryRow(f.ctx, `SELECT count(*) FROM notification_registrations`).Scan(&count) != nil || count != 0 {
		t.Fatal("capability declaration created a vendor registration")
	}
	if f.pool.QueryRow(f.ctx, `SELECT client_capabilities::text FROM sessions WHERE kind='emby' LIMIT 1`).Scan(&capabilities) != nil || strings.Contains(capabilities, "PushToken") {
		t.Fatal("vendor token persisted")
	}
	actor, err := f.users.Resolve(f.ctx, cookie.Value, "admin")
	if err != nil {
		t.Fatal(err)
	}
	key, err := f.users.CreateApplicationKey(f.ctx, actor, "Notification key test", "", identity.Client{Name: "Notification key"})
	if err != nil {
		t.Fatal(err)
	}
	keyHeaders := http.Header{"X-Emby-Token": {key.Token}, "X-Emby-Authorization": {`MediaBrowser Client="Notification test", Device="Test", DeviceId="notification-app-client", Version="1"`}}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Sessions/Notifications", nil, keyHeaders), 403)
}

func TestNotificationJournalCapacityRollsBackTheSourceAndCanBeRetried(t *testing.T) {
	f, cookie, csrf, headers, user := notificationHTTPFixture(t)
	configureNotificationFixture(t, f, cookie, csrf)
	registerNotificationFixture(t, f, headers)
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('notification-source-library','Notification source','movies'); INSERT INTO items(id,library_id,name,sort_name,type) VALUES('notification-source-item','notification-source-library','Source','source','Movie')`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.library.SetFavorite(f.ctx, user, "notification-source-item", false); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(f.ctx, `DELETE FROM notification_source_events; UPDATE notification_journal_state SET sequence=512; UPDATE notification_registrations SET source_cursor=0;
	INSERT INTO notification_source_events(id,sequence,kind,refs) SELECT md5('notification-capacity-'||n::text),n,'CatalogInvalidated','[{"Kind":"Item","Id":"notification-source-item","LibraryId":"notification-source-library"}]'::jsonb FROM generate_series(1,512)n`); err != nil {
		t.Fatal(err)
	}
	var before, after string
	if f.pool.QueryRow(f.ctx, `SELECT row_to_json(d)::text FROM user_item_data d WHERE user_id=$1 AND item_id='notification-source-item'`, user).Scan(&before) != nil {
		t.Fatal("read source state")
	}
	if _, err := f.app.library.SetFavorite(f.ctx, user, "notification-source-item", true); !errors.Is(err, notificationjournal.ErrCapacity) {
		t.Fatalf("full journal did not return retryable capacity: %v", err)
	}
	if f.pool.QueryRow(f.ctx, `SELECT row_to_json(d)::text FROM user_item_data d WHERE user_id=$1 AND item_id='notification-source-item'`, user).Scan(&after) != nil || before != after {
		t.Fatal("journal failure committed half of a source mutation")
	}
	var count int
	if f.pool.QueryRow(f.ctx, `SELECT count(*) FROM notification_source_events`).Scan(&count) != nil || count != 512 {
		t.Fatal("failed transaction changed journal facts")
	}
	if _, err := f.pool.Exec(f.ctx, `UPDATE notification_registrations SET source_cursor=512`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.library.SetFavorite(f.ctx, user, "notification-source-item", true); err != nil {
		t.Fatal("source could not retry after journal capacity was released")
	}
	if f.pool.QueryRow(f.ctx, `SELECT count(*) FROM notification_source_events WHERE kind='UserDataInvalidated' AND user_id=$1`, user).Scan(&count) != nil || count != 1 {
		t.Fatal("successful retry did not atomically retain its owner marker")
	}
}

func TestNotificationSourceMarkersDistinguishDecimalItemsAndEntities(t *testing.T) {
	f, cookie, csrf, headers, user := notificationHTTPFixture(t)
	configureNotificationFixture(t, f, cookie, csrf)
	registerNotificationFixture(t, f, headers)
	if _, err := f.pool.Exec(f.ctx, `INSERT INTO libraries(id,name,collection_type) VALUES('notification-typed-library','Typed','music'); INSERT INTO items(id,library_id,name,sort_name,type) VALUES('123','notification-typed-library','Physical numeric item','physical','Audio')`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.library.SetFavoriteFor(f.ctx, library.Subject{UserID: user}, "123", true); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if f.pool.QueryRow(f.ctx, `SELECT refs FROM notification_source_events WHERE user_id=$1 ORDER BY sequence DESC LIMIT 1`, user).Scan(&raw) != nil {
		t.Fatal("read typed journal marker")
	}
	var refs []notificationjournal.Reference
	if json.Unmarshal(raw, &refs) != nil || len(refs) != 1 || refs[0].Kind != "Item" || refs[0].ID != "123" {
		t.Fatal("opaque numeric item was converted to an entity notification")
	}
	if err := f.app.notificationRuntime.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
