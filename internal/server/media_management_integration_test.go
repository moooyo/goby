//go:build linux

package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"testing"
)

func requireHTTPMediaDeletionBinding(t *testing.T, s *streamHTTPFixture, item streamHTTPItem) {
	t.Helper()
	var bound bool
	if err := s.f.pool.QueryRow(s.f.ctx, `SELECT bool_and(storage_binding IS NOT NULL) FROM library_roots WHERE library_id=$1`, item.libraryID).Scan(&bound); err != nil {
		t.Fatal(err)
	}
	if !bound {
		t.Skip("safe deletion requires persistent root binding support")
	}
}

func TestHTTPMediaDeleteInfoAndPhysicalDeleteRequireCurrentPermission(t *testing.T) {
	s := newStreamHTTPFixture(t)
	path := "/emby/Items/" + s.video.id
	expectStreamStatus(t, s.request(t, http.MethodDelete, path, s.token, nil, nil), http.StatusForbidden)
	if _, err := os.Stat(s.video.path); err != nil {
		t.Fatalf("denied deletion touched source: %v", err)
	}
	requireHTTPMediaDeletionBinding(t, s, s.video)
	if _, err := s.f.pool.Exec(s.f.ctx, `UPDATE users SET policy=policy||'{"EnableContentDeletion":true}' WHERE id=$1`, s.viewerID); err != nil {
		t.Fatal(err)
	}
	info := s.request(t, http.MethodGet, path+"/DeleteInfo", s.token, nil, nil)
	expectStreamStatus(t, info, http.StatusOK)
	var body struct{ Paths []string }
	if err := json.Unmarshal(info.body, &body); err != nil || len(body.Paths) != 1 || body.Paths[0] != s.video.path {
		t.Fatalf("delete info did not identify the selected file: %+v %v", body, err)
	}
	deleted := s.request(t, http.MethodDelete, path, s.token, nil, nil)
	expectStreamStatus(t, deleted, http.StatusOK)
	if len(deleted.body) != 0 {
		t.Fatal("successful deletion must have an empty body")
	}
	if _, err := os.Stat(s.video.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source was not physically removed: %v", err)
	}
	if data, err := os.ReadFile(s.audio.path); err != nil || !bytes.Equal(data, s.audio.data) {
		t.Fatal("unrelated media was affected")
	}
	expectStreamStatus(t, s.request(t, http.MethodGet, path+"/File", s.token, nil, nil), http.StatusNotFound)
	expectStreamStatus(t, s.request(t, http.MethodDelete, path, s.token, nil, nil), http.StatusNotFound)
}

func TestHTTPMediaDeleteAliasAndRejectedSelectors(t *testing.T) {
	s := newStreamHTTPFixture(t)
	requireHTTPMediaDeletionBinding(t, s, s.video)
	if _, err := s.f.pool.Exec(s.f.ctx, `UPDATE users SET policy=policy||jsonb_build_object('EnableContentDeletionFromFolders',jsonb_build_array($2::text)) WHERE id=$1`, s.viewerID, s.video.libraryID); err != nil {
		t.Fatal(err)
	}
	path := "/Items/" + s.video.id + "/Delete"
	for _, suffix := range []string{"?Path=/etc/passwd", "?UserId=" + s.adminID, "?Recursive=true"} {
		expectStreamStatus(t, s.request(t, http.MethodPost, path+suffix, s.token, nil, nil), http.StatusBadRequest)
	}
	if data, err := os.ReadFile(s.video.path); err != nil || !bytes.Equal(data, s.video.data) {
		t.Fatal("invalid selector changed source")
	}
	expectStreamStatus(t, s.request(t, http.MethodPost, path, s.token, nil, nil), http.StatusOK)
}
