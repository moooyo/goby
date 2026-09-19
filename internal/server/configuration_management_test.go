package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/settings"
	"github.com/moooyo/goby/internal/systemevents"
	"github.com/moooyo/goby/internal/tasks"
)

func TestNamedManagementDecodingDefaultsBoundsAndClosedFields(t *testing.T) {
	for _, test := range []struct {
		section settings.ConfigurationSection
		body    string
		valid   bool
	}{
		{settings.ConfigurationSubtitles, `{}`, true},
		{settings.ConfigurationSubtitles, `{"DownloadLanguages":[],"DownloadMovieSubtitles":false}`, true},
		{settings.ConfigurationSubtitles, `{"DownloadLanguages":["en","en"]}`, false},
		{settings.ConfigurationSubtitles, `{"DownloadLanguages":null}`, false},
		{settings.ConfigurationSubtitles, `{"MaxConcurrent":2}`, false},
		{settings.ConfigurationTasks, `{"MaxConcurrent":16,"CacheRetentionDays":1,"CacheMaxEntries":1}`, true},
		{settings.ConfigurationTasks, `{"MaxConcurrent":0}`, false},
		{settings.ConfigurationTasks, `{"CacheMaxEntries":1000001}`, false},
		{settings.ConfigurationTasks, `{"CacheRetentionDays":null}`, false},
		{settings.ConfigurationTasks, `{"MaxConcurrent":1,"maxconcurrent":2}`, false},
	} {
		r := httptest.NewRequest(http.MethodPost, "/System/Configuration/"+string(test.section), strings.NewReader(test.body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		mutation, ok := decodeConfiguration(w, r, test.section)
		if ok != test.valid {
			t.Fatalf("named configuration acceptance mismatch for %s: %s", test.section, test.body)
		}
		if ok && test.section == settings.ConfigurationSubtitles && test.body == `{}` && mutation.Subtitles.DownloadLanguages[0] != "en" {
			t.Fatal("named reset has different defaults")
		}
	}
}

func TestCompatibleSystemEventRoundTripAndUnsupportedSignalRejection(t *testing.T) {
	for _, event := range []systemevents.Event{systemevents.ServerStarted, systemevents.LibraryChanged, systemevents.ConfigurationChanged} {
		rules, err := parseEmbyTaskTriggers([]byte(`[{"Type":"SystemEventTrigger","SystemEvent":"`+string(event)+`","MaxRuntimeTicks":10000000}]`), "UTC")
		if err != nil || len(rules) != 1 || rules[0].SystemEvent != event {
			t.Fatalf("event rule did not parse: %v", err)
		}
		name := string(event)
		dto := embyTaskTriggerDTO(tasks.Trigger{Kind: "system_event", SystemEvent: &name})
		if dto["Type"] != "SystemEventTrigger" || dto["SystemEvent"] != name {
			t.Fatal("event rule did not project")
		}
	}
	for _, payload := range []string{`[{"Type":"SystemEventTrigger","SystemEvent":"DisplayConfigurationChange"}]`, `[{"Type":"SystemEventTrigger"}]`, `[{"Type":"StartupTrigger","SystemEvent":"ServerStarted"}]`} {
		if _, err := parseEmbyTaskTriggers([]byte(payload), "UTC"); err == nil {
			t.Fatal("unsupported or mixed system event shape accepted")
		}
	}
}
