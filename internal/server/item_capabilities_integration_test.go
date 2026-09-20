//go:build linux

package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestHTTPItemCapabilitiesFollowProjectionAndCurrentDownloadPolicy(t *testing.T) {
	s := newStreamHTTPFixture(t)
	list := "/emby/Users/" + s.viewerID + "/Items?Ids=" + s.video.id
	read := func(path string, detail bool) map[string]any {
		t.Helper()
		response := s.request(t, http.MethodGet, path, s.token, nil, nil)
		expectStreamStatus(t, response, http.StatusOK)
		var body map[string]any
		if err := json.Unmarshal(response.body, &body); err != nil {
			t.Fatal(err)
		}
		if detail {
			return body
		}
		items, ok := body["Items"].([]any)
		if !ok || len(items) != 1 {
			t.Fatalf("unexpected catalog response: %#v", body)
		}
		return items[0].(map[string]any)
	}
	for _, name := range []string{"CanDelete", "CanDownload"} {
		if _, exists := read(list, false)[name]; exists {
			t.Fatalf("default list unexpectedly included %s", name)
		}
	}
	projected := read(list+"&Fields=CanDelete,CanDownload&EnableImages=false", false)
	if projected["CanDelete"] != false || projected["CanDownload"] != true {
		t.Fatalf("capability projection was coupled to image loading: %#v", projected)
	}
	detail := read("/emby/Users/"+s.viewerID+"/Items/"+s.video.id, true)
	if detail["CanDelete"] != false || detail["CanDownload"] != true {
		t.Fatalf("detail omitted current capabilities: %#v", detail)
	}
	if _, err := s.f.pool.Exec(s.f.ctx, `UPDATE users SET policy=policy||'{"EnableContentDownloading":false}'::jsonb WHERE id=$1`, s.viewerID); err != nil {
		t.Fatal(err)
	}
	if got := read(list+"&Fields=CanDownload", false); got["CanDownload"] != false {
		t.Fatalf("subsequent projection retained a revoked download grant: %#v", got)
	}
	excluded := read(list+"&Fields=CanDelete,CanDownload&ExcludeFields=CanDownload&EnableImages=false", false)
	if _, exists := excluded["CanDownload"]; exists || excluded["CanDelete"] != false {
		t.Fatalf("final exclusion was lost after capabilities: %#v", excluded)
	}
}
