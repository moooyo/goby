//go:build linux

package server

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHTTPAttachmentDTOStreamURLDeliversAuthorizedFont(t *testing.T) {
	fixture := newSubtitleHTTPFixture(t)
	s := fixture.p.s
	if _, err := s.f.pool.Exec(s.f.ctx, `UPDATE items SET media=jsonb_set(media,'{Streams}',(media->'Streams') || '[{"Index":4,"CodecType":"attachment","Codec":"ttf","Filename":"font.ttf","MIMEType":"font/ttf"}]'::jsonb) WHERE id=$1`, s.video.id); err != nil {
		t.Fatal(err)
	}
	// The fixture models extraction separately from routing: the extractor still
	// borrows the catalog descriptor and enforces its normal byte/type limits.
	extractor := filepath.Join(t.TempDir(), "font-extractor")
	if err := os.WriteFile(extractor, []byte("#!/bin/sh\nprintf '\\000\\001\\000\\000fontdata'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	s.f.app.cfg.FFmpegPath = extractor
	attachment := subtitleHTTPTrack(t, fixture.detail(t), 4)
	path, ok := attachment["DeliveryUrl"].(string)
	if !ok || !strings.Contains(path, "/Attachments/4/Stream?") {
		t.Fatal("the actual item attachment DTO omitted its canonical stream URL")
	}
	response := s.request(t, http.MethodGet, path, "", nil, nil)
	expectSubtitleHTTPStatus(t, response, http.StatusOK)
	if response.header.Get("Content-Type") != "font/ttf" || !bytes.Equal(response.body, []byte("\x00\x01\x00\x00fontdata")) {
		t.Fatal("the attachment DTO URL did not deliver the selected font")
	}
	conditional := http.Header{"If-None-Match": {response.header.Get("ETag")}}
	expectSubtitleHTTPStatus(t, s.request(t, http.MethodHead, path, "", conditional, nil), http.StatusNotModified)
	s.setPolicy(t, s.viewerID, false, []string{s.video.libraryID})
	expectSubtitleHTTPStatus(t, s.request(t, http.MethodGet, path, "", conditional, nil), http.StatusForbidden)
}
