//go:build linux

package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
)

func runtimePlaybackPolicyHTTPLogin(t *testing.T, fixture *streamHTTPFixture, name, password, deviceID string) map[string]any {
	t.Helper()
	body, err := json.Marshal(map[string]string{"Username": name, "Pw": password})
	if err != nil {
		t.Fatal("encode runtime policy login")
	}
	request, err := http.NewRequestWithContext(fixture.f.ctx, http.MethodPost,
		fixture.server.URL+"/emby/Users/AuthenticateByName", bytes.NewReader(body))
	if err != nil {
		t.Fatal("construct runtime policy login")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Emby-Client", "Runtime Policy Integration")
	request.Header.Set("X-Emby-Device-Id", deviceID)
	request.Header.Set("X-Emby-Device-Name", "Runtime Policy Fixture")
	request.Header.Set("X-Emby-Client-Version", "1.0")
	response, err := fixture.client.Do(request)
	if err != nil {
		t.Fatalf("runtime policy login transport failed (%T)", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("runtime policy login status = %d, want 200", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		t.Fatal("read runtime policy login response")
	}
	var result map[string]any
	if json.Unmarshal(raw, &result) != nil || result == nil {
		t.Fatal("runtime policy login did not return an object")
	}
	if bytes.Contains(raw, []byte("runtime-policy-private-marker")) {
		t.Fatal("runtime policy login exposed an opaque stored policy field")
	}
	return result
}

func assertRuntimePlaybackPolicyItem(t *testing.T, raw []byte, itemID string) {
	t.Helper()
	var item struct {
		ID           string `json:"Id"`
		MediaSources []struct {
			SupportsDirectPlay   *bool
			SupportsDirectStream *bool
			SupportsTranscoding  *bool
		}
	}
	if json.Unmarshal(raw, &item) != nil || item.ID != itemID || len(item.MediaSources) != 1 {
		t.Fatal("runtime policy browse lost the authorized item or its original source projection")
	}
	source := item.MediaSources[0]
	for _, permission := range []*bool{source.SupportsDirectPlay, source.SupportsDirectStream, source.SupportsTranscoding} {
		if permission == nil || *permission {
			t.Fatal("runtime policy browse advertised playback despite a malformed playback flag")
		}
	}
}

func TestHTTPRuntimeMalformedPlaybackPolicyRetainsLoginAndBrowseWithoutGrantingPlayback(t *testing.T) {
	fixture := newStreamHTTPFixture(t)
	f := fixture.f
	videoURL := "/emby/Videos/" + fixture.video.id + "/stream?Static=true"
	initial, err := f.users.ResolveWithPeer(f.ctx, fixture.token, "emby", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	baseline := fixture.request(t, http.MethodGet, videoURL, fixture.token, nil, nil)
	expectStreamStatus(t, baseline, http.StatusOK)
	if !bytes.Equal(baseline.body, fixture.video.data) {
		t.Fatal("the initial fixture did not deliver its authorized original media")
	}
	other, err := f.users.CreateUser(f.ctx, "Runtime Policy Other", "runtime-other-password", false)
	if err != nil {
		t.Fatal(err)
	}
	fixture.setPolicy(t, other.ID, true, []string{fixture.audio.libraryID})
	otherLogin := runtimePlaybackPolicyHTTPLogin(t, fixture, other.Name, "runtime-other-password", "runtime-policy-other")
	otherToken := stringValue(t, otherLogin, "AccessToken")
	encoded, err := json.Marshal(map[string]any{
		"EnableAllFolders": false, "EnabledFolders": []string{fixture.video.libraryID},
		"EnableMediaPlayback": "false", "PrivateRuntimeMarker": "runtime-policy-private-marker",
	})
	if err != nil {
		t.Fatal(err)
	}
	setHTTPUserPolicy(t, f, fixture.viewerID, string(encoded))
	readStored := func() (string, int64) {
		t.Helper()
		var raw string
		var revision int64
		if err := f.pool.QueryRow(f.ctx, "SELECT policy::text, management_revision FROM users WHERE id=$1", fixture.viewerID).Scan(&raw, &revision); err != nil {
			t.Fatal("read runtime policy storage snapshot")
		}
		return raw, revision
	}
	before, beforeRevision := readStored()
	login := runtimePlaybackPolicyHTTPLogin(t, fixture, "Stream Viewer", "stream-viewer-password", "runtime-policy-new")
	if policy := objectValue(t, objectValue(t, login, "User"), "Policy"); policy["EnableMediaPlayback"] != false {
		t.Fatal("login projection did not conservatively deny the malformed playback flag")
	}
	newToken := stringValue(t, login, "AccessToken")
	newPrincipal, err := f.users.ResolveWithPeer(f.ctx, newToken, "emby", "127.0.0.1")
	if err != nil {
		t.Fatalf("the new runtime-policy login did not resolve: %v", err)
	}
	for _, test := range []struct {
		name, token string
		principal   identity.Principal
	}{
		{"existing-token", fixture.token, initial},
		{"new-token", newToken, newPrincipal},
	} {
		t.Run(test.name, func(t *testing.T) {
			refreshed, err := f.users.RevalidateSession(f.ctx, test.principal)
			if err != nil || refreshed.SessionID != test.principal.SessionID || refreshed.User.ID != fixture.viewerID {
				t.Fatalf("continuous authentication rejected an otherwise valid login: %v", err)
			}
			policy, err := identity.ParseRuntimePolicy(refreshed.User.Policy)
			if err != nil || policy.EnableMediaPlayback || policy.EnableAllFolders || len(policy.EnabledFolders) != 1 || policy.EnabledFolders[0] != fixture.video.libraryID {
				t.Fatalf("continuous revalidation lost browse scope or granted playback: %v", err)
			}
			detail := fixture.request(t, http.MethodGet, "/emby/Users/"+fixture.viewerID+"/Items/"+fixture.video.id, test.token, nil, nil)
			expectStreamStatus(t, detail, http.StatusOK)
			assertRuntimePlaybackPolicyItem(t, detail.body, fixture.video.id)
			query := url.Values{"Ids": {fixture.video.id + "," + fixture.audio.id}, "Recursive": {"true"}, "Fields": {"MediaSources"}}
			browse := fixture.request(t, http.MethodGet, "/emby/Users/"+fixture.viewerID+"/Items?"+query.Encode(), test.token, nil, nil)
			expectStreamStatus(t, browse, http.StatusOK)
			var page struct {
				Items            []json.RawMessage
				TotalRecordCount int
			}
			if json.Unmarshal(browse.body, &page) != nil || page.TotalRecordCount != 1 || len(page.Items) != 1 {
				t.Fatal("runtime policy listing changed the valid folder allowlist or its count")
			}
			assertRuntimePlaybackPolicyItem(t, page.Items[0], fixture.video.id)
			item, err := f.app.library.GetItemFor(f.ctx, library.Subject{UserID: refreshed.User.ID}, fixture.video.id)
			if err != nil || item.CanPlay {
				t.Fatalf("authorized catalog item did not retain CanPlay=false: %v", err)
			}
			expectStreamStatus(t, fixture.request(t, http.MethodGet,
				"/emby/Users/"+fixture.viewerID+"/Items/"+fixture.audio.id, test.token, nil, nil), http.StatusNotFound)
			denied := fixture.request(t, http.MethodGet, videoURL, test.token, nil, nil)
			expectStreamStatus(t, denied, http.StatusForbidden)
			if denied.header.Get("ETag") != "" || denied.header.Get("Content-Range") != "" || bytes.Equal(denied.body, fixture.video.data) {
				t.Fatal("denied original playback exposed a source validator or original bytes")
			}
		})
	}
	// The malformed flag belongs to one account; the other account keeps its
	// independent playable audio library and cannot browse the viewer's video.
	otherDetail := fixture.request(t, http.MethodGet, "/emby/Users/"+other.ID+"/Items/"+fixture.audio.id, otherToken, nil, nil)
	expectStreamStatus(t, otherDetail, http.StatusOK)
	expectStreamStatus(t, fixture.request(t, http.MethodGet, "/emby/Users/"+other.ID+"/Items/"+fixture.video.id, otherToken, nil, nil), http.StatusNotFound)
	otherStream := fixture.request(t, http.MethodGet, "/emby/Audio/"+fixture.audio.id+"/stream?Static=true", otherToken, nil, nil)
	expectStreamStatus(t, otherStream, http.StatusOK)
	if !bytes.Equal(otherStream.body, fixture.audio.data) {
		t.Fatal("an unrelated user's valid playback policy was weakened")
	}
	after, afterRevision := readStored()
	if before != after || beforeRevision != afterRevision {
		t.Fatal("login, browsing, or revalidation rewrote the stored malformed policy")
	}
	var stored map[string]json.RawMessage
	if json.Unmarshal([]byte(after), &stored) != nil || string(stored["EnableMediaPlayback"]) != `"false"` {
		t.Fatal("runtime policy reads replaced the malformed stored value")
	}
}
