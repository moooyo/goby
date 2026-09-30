package server

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/systemstatus"
)

func TestHTTPAdminSystemStatusRequiresCurrentNativeAdministrator(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, _ := f.adminLogin(t)
	reader := &systemStatusTestReader{}
	f.app.hostStatus = reader
	path := "/admin/v1/system/status"
	expectStatus(t, f.request(t, http.MethodGet, path, nil, nil), http.StatusUnauthorized)
	emby := f.embyLogin(t, "Administrator", "administrator-password")
	embyToken := stringValue(t, emby, "AccessToken")
	actor, err := f.users.ResolveEmby(f.ctx, embyToken)
	if err != nil {
		t.Fatal("resolve isolated administrator")
	}
	for _, token := range []string{embyToken} {
		expectStatus(t, f.request(t, http.MethodGet, path, nil, http.Header{"X-Emby-Token": {token}}), http.StatusUnauthorized)
		expectStatus(t, f.request(t, http.MethodGet, path, nil, nil, &http.Cookie{Name: sessionCookie, Value: token}), http.StatusUnauthorized)
	}
	if reader.calls != 0 {
		t.Fatal("non-native credentials observed private host telemetry")
	}
	response := f.request(t, http.MethodGet, path, nil, nil, cookie)
	expectStatus(t, response, http.StatusOK)
	if response.Header().Get("Cache-Control") != "no-store" || reader.calls != 1 {
		t.Fatal("native status did not retain private response caching policy")
	}
	for _, query := range []string{"?", "?Path=private"} {
		expectStatus(t, f.request(t, http.MethodGet, path+query, nil, nil, cookie), http.StatusBadRequest)
	}
	if reader.calls != 1 {
		t.Fatal("query selected a host observation")
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_administrator=false WHERE id=$1", actor.User.ID); err != nil {
		t.Fatal("demote isolated administrator")
	}
	denied := f.request(t, http.MethodGet, path, nil, nil, cookie)
	if denied.Code != http.StatusUnauthorized && denied.Code != http.StatusForbidden {
		t.Fatal("demoted administrator retained telemetry authority")
	}
	if reader.calls != 1 {
		t.Fatal("demoted administrator sampled host telemetry")
	}
	if _, err := f.pool.Exec(f.ctx, "UPDATE users SET is_administrator=true WHERE id=$1", actor.User.ID); err != nil {
		t.Fatal("restore isolated administrator")
	}
	if err := f.users.Revoke(f.ctx, cookie.Value); err != nil {
		t.Fatal("revoke isolated administrator")
	}
	expectStatus(t, f.request(t, http.MethodGet, path, nil, nil, cookie), http.StatusUnauthorized)
	if reader.calls != 1 {
		t.Fatal("revoked administrator sampled host telemetry")
	}
}

func TestHTTPAdminSystemStatusReportsNativeHostAndConfiguredVolume(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	cookie, _ := f.adminLogin(t)
	root := t.TempDir()
	f.app.hostStatus = systemstatus.New([]string{root})
	var value adminSystemStatusDTO
	deadline := time.Now().Add(3 * time.Second)
	for {
		response := f.request(t, http.MethodGet, "/admin/v1/system/status", nil, nil, cookie)
		expectStatus(t, response, http.StatusOK)
		if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		if value.Memory.TotalBytes != nil && value.Storage.TotalBytes != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("native host sample did not complete")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if value.Timestamp.IsZero() || value.Host.CPUCount < 1 || value.Memory.TotalBytes == nil || value.Memory.UsedBytes == nil ||
		!value.Storage.Complete || value.Storage.TotalBytes == nil || *value.Storage.TotalBytes == 0 || len(value.Storage.Volumes) != 1 ||
		value.Storage.Volumes[0].Path != root || !value.Storage.Volumes[0].Available {
		t.Fatalf("native status omitted observed host facts: %+v", value)
	}
}
