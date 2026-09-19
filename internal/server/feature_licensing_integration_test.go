//go:build linux

package server

import (
	"net/http"
	"testing"
)

func TestHTTPFeatureLicensingPreservesUserAndLibraryAuthority(t *testing.T) {
	f := newAdminMetadataHTTPFixture(t)
	headers := http.Header{"X-Emby-Token": {f.viewerToken}}
	registration := "/emby/Registrations/playback"
	assertFree := func(path string) {
		t.Helper()
		response := f.request(t, http.MethodGet, path, nil, headers)
		expectStatus(t, response, http.StatusOK)
		info := jsonObject(t, response)
		if info["IsRegistered"] != true || info["IsTrial"] != false {
			t.Fatalf("server-local feature has a license restriction: %#v", info)
		}
	}
	for _, path := range []string{registration, "/registrations/themes", "/EMBY/REGISTRATIONS/intro"} {
		assertFree(path)
		expectStatus(t, f.request(t, http.MethodGet, path, nil, nil), http.StatusUnauthorized)
		expectStatus(t, f.request(t, http.MethodGet, path, nil, nil, f.cookie), http.StatusUnauthorized)
	}

	// A license check cannot turn off current user, playback, or catalog policy.
	setHTTPUserPolicy(t, f.serverFixture, f.viewerID, `{"EnableMediaPlayback":false,"EnableAllFolders":false,"EnabledFolders":[]}`)
	assertFree(registration)
	user := jsonObject(t, f.request(t, http.MethodGet, "/emby/Users/"+f.viewerID, nil, headers))
	policy := objectValue(t, user, "Policy")
	if policy["EnableMediaPlayback"] != false || policy["EnableAllFolders"] != false {
		t.Fatal("license response changed user permissions")
	}
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Users/"+f.viewerID+"/Items/"+f.itemID, nil, headers), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, "/admin/v1/items/"+f.itemID+"/metadata", nil, headers), http.StatusUnauthorized)

	// Declaring no license fee must not implement excluded commercial features.
	assertFree("/emby/Registrations/dvr")
	expectStatus(t, f.request(t, http.MethodGet, "/emby/LiveTv/Timers", nil, headers), http.StatusNotFound)
	expectStatus(t, f.request(t, http.MethodGet, "/emby/Dlna/ProfileInfos", nil, headers), http.StatusNotFound)

	setHTTPUserPolicy(t, f.serverFixture, f.viewerID, `{"EnableAllDevices":false,"EnabledDevices":[]}`)
	expectStatus(t, f.request(t, http.MethodGet, registration, nil, headers), http.StatusUnauthorized)
	setHTTPUserPolicy(t, f.serverFixture, f.viewerID, `{}`)
	if err := f.users.Revoke(f.ctx, f.viewerToken); err != nil {
		t.Fatal(err)
	}
	expectStatus(t, f.request(t, http.MethodGet, registration, nil, headers), http.StatusUnauthorized)
}
