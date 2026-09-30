package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/systemstatus"
	"github.com/moooyo/goby/internal/transcode"
)

type systemStatusTestReader struct{ calls int }

func (reader *systemStatusTestReader) Snapshot() systemstatus.Snapshot {
	reader.calls++
	return systemstatus.Snapshot{Timestamp: time.Date(2026, 9, 30, 1, 2, 3, 0, time.UTC), UptimeSeconds: 123,
		Host:    systemstatus.Host{OS: "linux", Architecture: "amd64", CPUCount: 8},
		Storage: systemstatus.Storage{Volumes: []systemstatus.Volume{}}}
}

type systemStatusTestJobs struct {
	hlsJobs
	metrics transcode.Metrics
}

func (jobs *systemStatusTestJobs) Metrics() transcode.Metrics { return jobs.metrics }

func TestAdminSystemStatusReturnsKnownAndUnknownValues(t *testing.T) {
	reader := &systemStatusTestReader{}
	s := &Server{hostStatus: reader}
	w := httptest.NewRecorder()
	s.adminSystemStatus(w, httptest.NewRequest(http.MethodGet, "/admin/v1/system/status", nil))
	var result adminSystemStatusDTO
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || reader.calls != 1 || result.UptimeSeconds != 123 || result.Host.CPUCount != 8 ||
		result.CPU.UsagePercent != nil || result.Memory.UsedBytes != nil || result.Transcoding.Available ||
		result.Transcoding.Active == nil || *result.Transcoding.Active != 0 || result.Transcoding.Limit == nil || *result.Transcoding.Limit != 0 {
		t.Fatalf("status response lost observed or unavailable state: %s", w.Body.String())
	}
	var fields map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 7 {
		t.Fatalf("status leaked additional fields: %s", w.Body.String())
	}
}

func TestAdminSystemStatusRejectsSelectorsBeforeReadingHost(t *testing.T) {
	reader := &systemStatusTestReader{}
	s := &Server{hostStatus: reader}
	for _, query := range []string{"?", "?Path=/private", "?Limit=1", "?api_key=invalid"} {
		w := httptest.NewRecorder()
		s.adminSystemStatus(w, httptest.NewRequest(http.MethodGet, "/admin/v1/system/status"+query, nil))
		if w.Code != http.StatusBadRequest || reader.calls != 0 {
			t.Fatalf("query selector reached telemetry: %q %s", query, w.Body.String())
		}
	}
	s.hostStatus = nil
	w := httptest.NewRecorder()
	s.adminSystemStatus(w, httptest.NewRequest(http.MethodGet, "/admin/v1/system/status", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing collector fabricated observations: %s", w.Body.String())
	}
}

func TestAdminTranscodingMetricsKeepDisabledUnknownAndOccupiedDistinct(t *testing.T) {
	zero, active, hardware, software, limit := 0, 3, 1, 2, 7
	for _, test := range []struct {
		name   string
		server Server
		want   adminTranscodingMetrics
	}{
		{name: "disabled", want: adminTranscodingMetrics{Active: &zero, Limit: &zero, HardwareActive: &zero, SoftwareActive: &zero}},
		{name: "enabled engine missing", server: Server{cfg: config.Config{Transcoding: config.TranscodingConfig{Enabled: true}}}},
		{name: "unreported metrics", server: Server{hls: &hlsRuntime{}}},
		{name: "occupied slots", server: Server{hls: &hlsRuntime{manager: &systemStatusTestJobs{metrics: transcode.Metrics{Running: 3, Hardware: 1, Software: 2, MaxJobs: 7}}}},
			want: adminTranscodingMetrics{Available: true, Active: &active, Limit: &limit, HardwareActive: &hardware, SoftwareActive: &software}},
		{name: "inconsistent counters", server: Server{hls: &hlsRuntime{manager: &systemStatusTestJobs{metrics: transcode.Metrics{Running: 3, Hardware: 3, Software: 3, MaxJobs: 7}}}}},
		{name: "unknown capacity", server: Server{hls: &hlsRuntime{manager: &systemStatusTestJobs{}}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.server.adminTranscodingMetrics(); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("transcode observation = %+v, want %+v", got, test.want)
			}
		})
	}
}
