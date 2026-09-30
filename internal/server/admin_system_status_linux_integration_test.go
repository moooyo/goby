//go:build linux

package server

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/moooyo/goby/internal/identity"
)

// The application-key vault deliberately supports Linux only. Keep its real
// credential-isolation check alongside the cross-platform native-cookie tests.
func TestHTTPAdminSystemStatusRejectsApplicationKeys(t *testing.T) {
	f := newServerFixture(t)
	f.bootstrap(t)
	reader := &systemStatusTestReader{}
	f.app.hostStatus = reader
	emby := f.embyLogin(t, "Administrator", "administrator-password")
	embyToken := stringValue(t, emby, "AccessToken")
	f.users = identity.NewWithApplicationKeyVault(f.pool, identity.NewApplicationKeyVault(filepath.Join(t.TempDir(), "master.key")))
	f.app.identity = f.users
	actor, err := f.users.ResolveEmby(f.ctx, embyToken)
	if err != nil {
		t.Fatal("resolve isolated application-key owner")
	}
	key, err := f.users.CreateApplicationKey(f.ctx, actor, "System status isolation", "127.0.0.1", identity.Client{DeviceID: f.app.serverID})
	if err != nil {
		t.Fatal("create isolated application key")
	}
	path := "/admin/v1/system/status"
	expectStatus(t, f.request(t, http.MethodGet, path, nil, http.Header{"X-Emby-Token": {key.Token}}), http.StatusUnauthorized)
	expectStatus(t, f.request(t, http.MethodGet, path, nil, nil, &http.Cookie{Name: sessionCookie, Value: key.Token}), http.StatusUnauthorized)
	if reader.calls != 0 {
		t.Fatal("application credential observed private host telemetry")
	}
}
