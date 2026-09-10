package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/settings"
)

func TestConfigurationDTONameModesAndExactViewerJSON(t *testing.T) {
	custom, empty := "Configured name", ""
	for _, test := range []struct {
		mode    settings.ServerNameMode
		raw     *string
		name    any
		present bool
	}{
		{settings.ServerNameDeployment, nil, "Deployment name", true},
		{settings.ServerNameCustom, &custom, custom, true},
		{settings.ServerNameEmpty, &empty, "", true},
		{settings.ServerNameUnset, nil, nil, false},
	} {
		view := settings.Configuration{CanManage: true, StartupWizardCompleted: true,
			Snapshot: settings.Snapshot{Defaults: settings.Values{ServerName: "Deployment name"},
				Overrides: settings.Overrides{ServerName: test.raw}, ServerNameMode: test.mode, HostName: "Host name"}}
		value := serverConfigurationDTO(view)
		name, present := value["ServerName"]
		if name != test.name || present != test.present || value["IsStartupWizardCompleted"] != true {
			t.Fatal("configuration DTO lost configured name presence or startup state")
		}
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		r := httptest.NewRequest(method, "/emby/System/Configuration", nil)
		w := httptest.NewRecorder()
		configurationJSON(w, r, map[string]any{})
		if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json" || w.Header().Get("Content-Length") != "2" {
			t.Fatal("viewer configuration lost its exact two-byte JSON representation")
		}
		if method == http.MethodGet && w.Body.String() != "{}" || method == http.MethodHead && w.Body.Len() != 0 {
			t.Fatal("configuration GET or HEAD emitted an incorrect body")
		}
	}
}

func TestConfigurationDecoderRetainsPresenceAndAcceptsFieldCaseAliases(t *testing.T) {
	emptyName, customName := "", "Configured"
	for _, test := range []struct {
		section         settings.ConfigurationSection
		body, mediaType string
		present         bool
		name            *string
		width           int
	}{
		{settings.ConfigurationPartial, `{}`, "application/json", false, nil, 0},
		{settings.ConfigurationPartial, `{"SERVERNAME":null}`, "application/json", true, nil, 0},
		{settings.ConfigurationFull, `{"ServerName":""}`, "application/json", true, &emptyName, 0},
		{settings.ConfigurationFull, `{"serverName":"Configured","isStartupWizardCompleted":true}`, "application/json; charset=UTF-8", true, &customName, 0},
		{settings.ConfigurationEncoding, `{"TRANSCODINGMAXWIDTH":96}`, "application/octet-stream", false, nil, 96},
	} {
		r := httptest.NewRequest(http.MethodPost, "/emby/System/Configuration", strings.NewReader(test.body))
		r.Header.Set("Content-Type", test.mediaType)
		w := httptest.NewRecorder()
		value, ok := decodeConfiguration(w, r, test.section)
		if !ok || value.Section != test.section || value.ServerNamePresent != test.present || !reflect.DeepEqual(value.ServerName, test.name) || value.TranscodingMaxWidth != test.width {
			t.Fatal("configuration input lost the distinction between absent, null, empty, and configured values")
		}
	}
}

func TestConfigurationDecoderRejectsAmbiguousAndLossyInput(t *testing.T) {
	for _, test := range []struct {
		section         settings.ConfigurationSection
		body, mediaType string
		status          int
	}{
		{settings.ConfigurationFull, `{}`, "", 415},
		{settings.ConfigurationFull, `{}`, "application/octet-stream", 415},
		{settings.ConfigurationPartial, `{}`, "application/octet-stream", 415},
		{settings.ConfigurationEncoding, `{}`, "application/xml", 415},
		{settings.ConfigurationEncoding, `{}`, "application/json; charset=iso-8859-1", 415},
		{settings.ConfigurationEncoding, `{}`, "application/json; charset*=utf-8''utf-8", 415},
		{settings.ConfigurationEncoding, `{}`, "application/json; charset=utf-8; private-marker=1", 415},
		{settings.ConfigurationPartial, `null`, "application/json", 400},
		{settings.ConfigurationPartial, `[]`, "application/json", 400},
		{settings.ConfigurationPartial, `{} {}`, "application/json", 400},
		{settings.ConfigurationPartial, `{"ServerName":"first","servername":"second"}`, "application/json", 400},
		{settings.ConfigurationPartial, `{"ServerName":"first","\u0053erverName":"second"}`, "application/json", 400},
		{settings.ConfigurationPartial, `{"ServerName":"\ud800private-marker"}`, "application/json", 400},
		{settings.ConfigurationPartial, "{\"ServerName\":\"" + string([]byte{0xff}) + "\"}", "application/json", 400},
		{settings.ConfigurationPartial, `{"ServerName":9}`, "application/json", 400},
		{settings.ConfigurationPartial, `{"IsStartupWizardCompleted":null}`, "application/json", 400},
		{settings.ConfigurationPartial, `{"private-marker":"private-marker"}`, "application/json", 400},
		{settings.ConfigurationFull, `{"TranscodingMaxWidth":96}`, "application/json", 400},
		{settings.ConfigurationEncoding, `{"ServerName":"private-marker"}`, "application/json", 400},
		{settings.ConfigurationEncoding, `{"TranscodingMaxWidth":"96"}`, "application/json", 400},
		{settings.ConfigurationEncoding, `{"TranscodingMaxWidth":null}`, "application/json", 400},
		{settings.ConfigurationEncoding, `{"TranscodingMaxWidth":1.5}`, "application/json", 400},
		{settings.ConfigurationEncoding, `{"TranscodingMaxWidth":8193}`, "application/json", 400},
		{settings.ConfigurationPartial, strings.Repeat(" ", maxConfigurationBodyBytes) + `{}`, "application/json", 400},
	} {
		r := httptest.NewRequest(http.MethodPost, "/emby/System/Configuration", strings.NewReader(test.body))
		if test.mediaType != "" {
			r.Header.Set("Content-Type", test.mediaType)
		}
		w := httptest.NewRecorder()
		if _, ok := decodeConfiguration(w, r, test.section); ok || w.Code != test.status || strings.Contains(w.Body.String(), "private-marker") {
			t.Fatalf("configuration parser accepted invalid input, reflected rejected data, or returned status %d, want %d", w.Code, test.status)
		}
	}
}

func TestConfigurationClosedQueriesAndNamedRegistry(t *testing.T) {
	for _, query := range []string{"ServerName=private-marker", "api_key=a&api_key=a", "Api_Key=a", "x=%zz", strings.Repeat("a", 4097)} {
		r := httptest.NewRequest(http.MethodGet, "/emby/System/Configuration", nil)
		r.URL.RawQuery = query
		w := httptest.NewRecorder()
		if configurationQuery(w, r) || w.Code != http.StatusBadRequest || strings.Contains(w.Body.String(), "private-marker") {
			t.Fatal("configuration query accepted state input or exposed a rejected field")
		}
	}
	for key, want := range map[string]int{"devices": 501, "DLNA": 501, "private-marker": 404, "../encoding": 404} {
		r := httptest.NewRequest(http.MethodGet, "/emby/System/Configuration/unknown", nil)
		r.SetPathValue("key", key)
		w := httptest.NewRecorder()
		if configurationSection(w, r) || w.Code != want || strings.Contains(w.Body.String(), "private-marker") {
			t.Fatal("named configuration registry accepted an unsupported name or treated it as a path")
		}
	}
	data, err := json.Marshal(encodingConfigurationDTO(settings.Snapshot{Encoding: settings.Encoding{TranscodingMaxWidth: 96}}))
	if err != nil || string(data) != `{"TranscodingMaxWidth":96}` {
		t.Fatal("named encoding DTO exposed an unrelated native setting")
	}
}
