package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/subtitle"
	"github.com/moooyo/goby/internal/transcode"
)

func TestSubtitleOptionsSupportASSAndSignedOffset(t *testing.T) {
	r := httptest.NewRequest("GET", "/?SubtitleOffsetTicks=-10000000&CopyTimestamps=true", nil)
	r.SetPathValue("SubtitleFileName", "Stream.ass")
	r.SetPathValue("Index", "5")
	index, options, err := readSubtitleOptions(r)
	if err != nil || index != 5 || options.Format != subtitle.FormatASS || options.OffsetTicks != -subtitle.TicksPerSecond || !options.CopyTimestamps {
		t.Fatalf("ASS request options = %d, %+v, %v", index, options, err)
	}
	r.URL.RawQuery = "SubtitleOffsetTicks=864000000001"
	if _, _, err := readSubtitleOptions(r); err == nil {
		t.Fatal("unbounded subtitle offset was accepted")
	}
}

func TestDeletedSubtitleRetiresOnlyAffectedConversionSessions(t *testing.T) {
	runtime := &hlsRuntime{sessions: make(map[string]*hlsSession), byKey: make(map[hlsKey]*hlsSession)}
	for _, test := range []struct {
		id, item, mode string
		index          int
	}{
		{"burn", "movie", "burn", 6}, {"manifest", "movie", "hls", 6},
		{"other-track", "movie", "burn", 7}, {"other-item", "other", "burn", 6}, {"off", "movie", "", 6},
	} {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		session := &hlsSession{id: test.id, ctx: ctx, cancel: cancel, key: hlsKey{scope: transcode.Scope{ItemID: test.item, AuthSessionID: test.id},
			plan: transcode.Plan{Subtitle: transcode.SubtitlePlan{Mode: test.mode, StreamIndex: test.index}}}}
		runtime.sessions[session.id], runtime.byKey[session.key] = session, session
	}
	server := &Server{hls: runtime}
	server.retireDeletedSubtitle("movie", 6)
	for _, id := range []string{"burn", "manifest"} {
		if runtime.sessions[id] != nil {
			t.Errorf("deleted subtitle kept conversion session %q", id)
		}
	}
	for _, id := range []string{"other-track", "other-item", "off"} {
		if runtime.sessions[id] == nil || runtime.sessions[id].closed {
			t.Errorf("subtitle deletion retired unrelated session %q", id)
		}
	}
}

func TestEmbeddedSubtitleAndFontDTOsPreserveIndexesAndUseAuthenticatedRoutes(t *testing.T) {
	item := library.Item{ID: "movie", Media: &media.Info{Streams: []media.Stream{
		{Index: 5, CodecType: "subtitle", Codec: "ass", IsTextSubtitleStream: true, Language: "jpn", IsDefault: true, IsForced: true},
		{Index: 8, CodecType: "attachment", Codec: "ttf", Filename: "font.ttf", MIMEType: "font/ttf"},
	}}}
	streams := itemMediaStreamsDTO(item)
	dto := map[string]any{"MediaStreams": streams}
	addSubtitleDeliveryCredentials(dto, item.ID, "test-token", map[int]string{5: "vtt"})
	if streams[0]["Index"] != 5 || streams[0]["IsExternal"] != false || streams[0]["IsForced"] != true || streams[0]["Language"] != "jpn" {
		t.Fatal("embedded subtitle source identity or language changed")
	}
	for index, suffix := range []string{"/Subtitles/5/0/Stream.vtt?api_key=test-token", "/Attachments/8/Stream?api_key=test-token"} {
		url, _ := streams[index]["DeliveryUrl"].(string)
		if !strings.HasSuffix(url, suffix) {
			t.Errorf("credentialed resource URL = %q", url)
		}
	}
}

func TestSubtitleAttachmentRoutesCoexistWithAuthenticatedImages(t *testing.T) {
	// Build the complete production route table so wildcard overlaps cannot be
	// hidden by registering subtitle routes on an isolated test ServeMux.
	handler := (&Server{}).Handler()
	generated, err := url.Parse(subtitleAttachmentURL("movie", 8, "test-token"))
	if err != nil || generated.Query().Get("api_key") != "test-token" || !strings.HasSuffix(generated.Path, "/Attachments/8/Stream") {
		t.Fatal("attachment DTO did not generate the canonical stream resource")
	}
	for _, test := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, generated.Path, http.StatusUnauthorized},
		{http.MethodHead, "/emby" + generated.Path, http.StatusUnauthorized},
		{http.MethodGet, strings.Replace("/emby"+generated.Path, "/Videos/", "/Items/", 1), http.StatusUnauthorized},
		{http.MethodGet, "/emby/Items/movie/Images/Primary/0", http.StatusUnauthorized},
		{http.MethodGet, "/emby/Items/movie/Images/Attachments/8", http.StatusUnauthorized},
		{http.MethodGet, strings.TrimSuffix("/emby"+generated.Path, "/Stream"), http.StatusNotFound},
		{http.MethodGet, strings.Replace(strings.TrimSuffix("/emby"+generated.Path, "/Stream"), "/Videos/", "/Items/", 1), http.StatusNotFound},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
		if response.Code != test.status {
			t.Errorf("%s %s returned %d, want %d", test.method, test.path, response.Code, test.status)
		}
	}
}
