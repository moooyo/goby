package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/settings"
)

func TestNativeSubtitleConfigurationDecodesASSOnlyWithoutDownloadReplacement(t *testing.T) {
	decode := func(body string) (settings.ConfigurationMutation, bool) {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, "/System/Configuration/subtitles", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		return decodeConfiguration(httptest.NewRecorder(), request, settings.ConfigurationSubtitles)
	}
	for _, enabled := range []string{"false", "true"} {
		mutation, ok := decode(`{"AllowAssToText":` + enabled + `}`)
		if !ok || mutation.AllowAssToText == nil || *mutation.AllowAssToText != (enabled == "true") || mutation.Subtitles != nil {
			t.Fatal("the ASS-only native editor replaced download policy or lost an explicit boolean")
		}
	}
	legacy, ok := decode(`{}`)
	if !ok || legacy.AllowAssToText != nil || legacy.Subtitles == nil || legacy.Subtitles.DownloadLanguages[0] != "en" {
		t.Fatal("a legacy empty subtitle writer lost its established download reset behavior")
	}
	complete, ok := decode(`{"DownloadLanguages":["fr"],"DownloadMovieSubtitles":false,"AllowAssToText":false}`)
	if !ok || complete.Subtitles == nil || complete.AllowAssToText == nil || *complete.AllowAssToText || complete.Subtitles.DownloadLanguages[0] != "fr" {
		t.Fatal("a complete fixed subtitle configuration lost independent output and download choices")
	}
	for _, body := range []string{`{"AllowAssToText":null}`, `{"AllowAssToText":1}`, `{"AllowAssToText":"false"}`, `{"AllowAssToText":false,"allowasstotext":true}`} {
		if _, ok := decode(body); ok {
			t.Fatal("an ambiguous or incorrectly typed native ASS control entered the configuration mutation")
		}
	}
}
