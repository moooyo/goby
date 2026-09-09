//go:build linux

package server

import (
	"bytes"
	"net/http"
	"testing"
)

func TestHTTPManagedPolicyChangesEnforceLibraryAndOriginalPlaybackAccess(t *testing.T) {
	s := newStreamHTTPFixture(t)
	f := s.f
	adminHeaders := http.Header{"X-CSRF-Token": {csrfToken(s.cookie.Value)}}
	viewerHeaders := http.Header{"X-Emby-Token": {s.token}}
	path := "/admin/v1/users/" + s.viewerID
	user := managedHTTPDetail(t, f, s.cookie, s.viewerID)
	save := func(change func(map[string]any)) {
		t.Helper()
		input := managedHTTPUpdateBody(user)
		change(input["Policy"].(map[string]any))
		response := f.request(t, http.MethodPut, path, input, adminHeaders, s.cookie)
		expectStatus(t, response, http.StatusOK)
		user = objectValue(t, jsonObject(t, response), "User")
	}
	stream := func(item streamHTTPItem) streamHTTPResponse {
		t.Helper()
		return s.request(t, http.MethodGet, "/emby/"+item.route+"/"+item.id+"/stream."+item.container+"?Static=true", s.token, nil, nil)
	}
	for _, item := range []streamHTTPItem{s.video, s.audio} {
		response := stream(item)
		expectStreamStatus(t, response, http.StatusOK)
		if !bytes.Equal(response.body, item.data) {
			t.Fatal("original fixture bytes changed before policy editing")
		}
	}

	save(func(policy map[string]any) {
		policy["EnableAllFolders"] = false
		policy["EnabledFolders"] = []string{s.video.libraryID}
	})
	views, total := responseItems(t, f.request(t, http.MethodGet, "/emby/Users/"+s.viewerID+"/Views", nil, viewerHeaders))
	if total != 1 || len(views) != 1 || views[0]["Id"] != s.video.libraryID {
		t.Fatal("native policy save did not restrict the existing client's library view")
	}
	expectStreamStatus(t, stream(s.video), http.StatusOK)
	deniedAudio := stream(s.audio)
	expectStreamStatus(t, deniedAudio, http.StatusNotFound)
	if bytes.Contains(deniedAudio.body, s.audio.data) {
		t.Fatal("revoked audio library exposed original bytes")
	}
	expectAPIError(t, f.request(t, http.MethodGet, "/emby/Users/"+s.viewerID+"/Items/"+s.audio.id, nil, viewerHeaders), http.StatusNotFound, "not_found", true)

	save(func(policy map[string]any) { policy["EnableMediaPlayback"] = false })
	deniedVideo := stream(s.video)
	expectStreamStatus(t, deniedVideo, http.StatusForbidden)
	if bytes.Contains(deniedVideo.body, s.video.data) {
		t.Fatal("playback-disabled account received original bytes")
	}
	// Playback permission is independent of browsing: the account can still
	// inspect its permitted library while playback is disabled.
	views, total = responseItems(t, f.request(t, http.MethodGet, "/emby/Users/"+s.viewerID+"/Views", nil, viewerHeaders))
	if total != 1 || len(views) != 1 {
		t.Fatal("playback denial unexpectedly removed authorized browsing")
	}

	save(func(policy map[string]any) {
		policy["EnableMediaPlayback"] = true
		policy["EnableAllFolders"] = true
	})
	for _, item := range []streamHTTPItem{s.video, s.audio} {
		response := stream(item)
		expectStreamStatus(t, response, http.StatusOK)
		if !bytes.Equal(response.body, item.data) {
			t.Fatal("restored policy changed original fixture bytes")
		}
	}
}
