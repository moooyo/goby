//go:build linux

package server

import (
	"bytes"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/settings"
	"github.com/moooyo/goby/internal/subtitle"
)

const assConversionHTTPDocument = "[Script Info]\nTitle: Styled HTTP captions\nScriptType: v4.00+\nPlayResX: 1280\nPlayResY: 720\n" +
	"[V4+ Styles]\nFormat: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n" +
	"Style: Default,Example Font,24,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,-1,0,0,100,100,0,0,1,1,0,2,10,10,10,1\n" +
	"[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n" +
	"Dialogue: 0,0:00:01.00,0:00:03.50,Default,,0,0,0,,{\\pos(50,60)}Styled caption\n"

func TestHTTPAssToTextPolicyPreservesExplicitConversionsAndStyledSource(t *testing.T) {
	h := newHLSHTTPFixture(t)
	sidecar := strings.TrimSuffix(h.path, filepath.Ext(h.path)) + ".en.ass"
	if err := os.WriteFile(sidecar, []byte(assConversionHTTPDocument), 0600); err != nil {
		t.Fatal(err)
	}
	embedded := filepath.Join(t.TempDir(), "embedded-ass.mkv")
	hlsHTTPMediaCommand(t, h.ffmpeg, "-hide_banner", "-nostdin", "-v", "error", "-i", h.path, "-i", sidecar,
		"-map", "0:v:0", "-map", "1:0", "-c:v", "copy", "-c:s", "copy", "-f", "matroska", embedded)
	data, err := os.ReadFile(embedded)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.path, data, 0600); err != nil {
		t.Fatal(err)
	}
	(&streamHTTPFixture{f: h.f}).rescan(t, h.libraryID)
	h.item, err = h.f.app.library.GetItem(h.f.ctx, h.accounts.viewer.userID, h.item.ID)
	if err != nil || h.item.Media == nil || len(h.item.Subtitles) != 1 || h.item.Subtitles[0].Codec != "ass" {
		t.Fatal("the styled embedded and sidecar subtitles were not indexed")
	}
	embeddedIndex := -1
	for _, stream := range h.item.Media.Streams {
		if stream.CodecType == "subtitle" && stream.Codec == "ass" && !stream.IsExternal {
			embeddedIndex = stream.Index
		}
	}
	if embeddedIndex < 0 {
		t.Fatal("the actual media source did not contain an embedded ASS stream")
	}
	actor, err := h.f.users.Resolve(h.f.ctx, h.accounts.admin.headers.Get("X-Emby-Token"), "emby")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := h.f.app.settings.Snapshot()
	options := settings.RuntimeSubtitleOptions{AllowAssToText: false}
	if _, err := h.f.app.settings.Update(h.f.ctx, settings.Actor{Principal: actor, Audience: identity.AdministratorEmby}, settings.UpdateRequest{
		Revision: snapshot.Revision, Overrides: snapshot.Overrides, NameMode: &snapshot.ServerNameMode,
		Runtime: &settings.RuntimeUpdate{SubtitleOptions: settings.Change[settings.RuntimeSubtitleOptions]{Present: true, Value: &options}},
	}); err != nil {
		t.Fatal(err)
	}
	if !h.f.app.currentSettingsSnapshot().DisableAssToText {
		t.Fatal("the committed subtitle negotiation preference was not active")
	}
	query := url.Values{"api_key": {h.accounts.viewer.headers.Get("X-Emby-Token")}}
	var conditionalTarget, conditionalETag string
	for _, index := range []int{embeddedIndex, h.item.Subtitles[0].Index} {
		for _, format := range []subtitle.Format{subtitle.FormatSRT, subtitle.FormatWebVTT, subtitle.FormatASS} {
			target := "/emby/Items/" + h.item.ID + "/" + media.SourceID(h.item.ID) + "/Subtitles/" + strconv.Itoa(index) + "/Stream." + string(format) + "?" + query.Encode()
			response := h.request(t, http.MethodGet, target, nil, nil)
			expectHLSHTTPStatus(t, response, http.StatusOK)
			if format == subtitle.FormatASS {
				if !bytes.Contains(response.body, []byte("Example Font")) || !bytes.Contains(response.body, []byte(`{\pos(50,60)}`)) || !bytes.Contains(response.body, []byte("PlayResX: 1280")) {
					t.Fatal("explicit styled delivery lost font, position, or script metadata")
				}
			} else {
				document, err := subtitle.Parse(response.body, format)
				if err != nil || len(document.Cues) != 1 || document.Cues[0].StartTicks != media.TicksPerSecond ||
					document.Cues[0].EndTicks != 35_000_000 || !strings.Contains(document.Cues[0].Text, "Styled caption") {
					t.Fatal("an explicit text request lost the actual caption or its clock")
				}
				conditionalTarget, conditionalETag = target, response.header.Get("ETag")
			}
		}
	}
	if conditionalETag == "" {
		t.Fatal("the explicit subtitle response did not retain conditional delivery metadata")
	}
	conditional := http.Header{"If-None-Match": {conditionalETag}}
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, conditionalTarget, nil, conditional), http.StatusNotModified)
	// The preference controls negotiation, while existing source and principal
	// authorization still apply before an explicit or conditional conversion.
	h.policy(t, false)
	expectHLSHTTPStatus(t, h.request(t, http.MethodGet, conditionalTarget, nil, conditional), http.StatusForbidden)
}
