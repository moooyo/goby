package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/transcode"
)

type transcodingHealthTestJobs struct {
	hlsJobs
	status transcode.Health
}

func (jobs *transcodingHealthTestJobs) Health() transcode.Health { return jobs.status }

func TestTranscodingCapabilitiesSeparateConfigurationAndAvailability(t *testing.T) {
	for _, test := range []struct {
		name       string
		configured bool
		runtime    *hlsRuntime
		available  bool
		reason     string
	}{
		{name: "disabled", reason: "disabled"},
		{name: "missing engine", configured: true, reason: "engine_unavailable"},
		{name: "unknown engine health", runtime: &hlsRuntime{}, reason: "engine_status_unavailable"},
		{name: "healthy", runtime: &hlsRuntime{manager: &transcodingHealthTestJobs{status: transcode.Health{Available: true, Code: "ready"}}}, available: true, reason: "ready"},
		{name: "cache failure", runtime: &hlsRuntime{manager: &transcodingHealthTestJobs{status: transcode.Health{Code: "cache_unavailable"}}}, reason: "cache_unavailable"},
		{name: "closed manager", runtime: &hlsRuntime{manager: &transcodingHealthTestJobs{status: transcode.Health{Code: "manager_closed"}}}, reason: "manager_closed"},
		{name: "closing runtime", runtime: &hlsRuntime{closing: true, manager: &transcodingHealthTestJobs{status: transcode.Health{Available: true, Code: "ready"}}}, reason: "manager_closed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := &Server{cfg: config.Config{Transcoding: config.TranscodingConfig{Enabled: test.configured,
				Hardware: transcode.Hardware{Decode: "vaapi", Encode: "vaapi"}}}, hls: test.runtime}
			response := httptest.NewRecorder()
			s.capabilities(response, httptest.NewRequest(http.MethodGet, "/admin/v1/capabilities", nil))
			var body struct {
				Features    map[string]bool
				Transcoding transcodingStatus
				Hardware    struct {
					Configured, Verified bool
					Decode, Encode       []string
				}
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusOK || body.Transcoding.Configured != (test.configured || test.runtime != nil) ||
				body.Transcoding.Available != test.available || body.Transcoding.Reason != test.reason || body.Features["Transcoding"] != test.available {
				t.Fatalf("capabilities misreported engine status: %s", response.Body.String())
			}
			if !body.Hardware.Configured || body.Hardware.Verified || body.Features["HardwareDecoding"] || body.Features["HardwareEncoding"] ||
				len(body.Hardware.Decode) != 1 || body.Hardware.Decode[0] != "vaapi" || len(body.Hardware.Encode) != 1 || body.Hardware.Encode[0] != "vaapi" {
				t.Fatalf("configured hardware was lost or presented as verified: %s", response.Body.String())
			}
		})
	}
}

func TestTranscodingFailureStopsReadinessWithSpecificReason(t *testing.T) {
	for _, code := range []string{"cache_unavailable", "manager_closed"} {
		t.Run(code, func(t *testing.T) {
			s := &Server{hls: &hlsRuntime{manager: &transcodingHealthTestJobs{status: transcode.Health{Code: code}}}}
			response := httptest.NewRecorder()
			s.ready(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
			if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "transcoding_"+code) {
				t.Fatalf("unavailable engine passed readiness or lost its cause: %s", response.Body.String())
			}
		})
	}
}

func TestTranscodingHealthyAndDisabledEnginesPermitReadiness(t *testing.T) {
	f := newServerFixture(t)
	for _, enabled := range []bool{false, true} {
		s := &Server{db: f.pool, library: f.app.library}
		if enabled {
			s.hls = &hlsRuntime{manager: &transcodingHealthTestJobs{status: transcode.Health{Available: true, Code: "ready"}}}
		}
		response := httptest.NewRecorder()
		s.ready(response, httptest.NewRequest(http.MethodGet, "/readyz", nil).WithContext(f.ctx))
		if response.Code != http.StatusOK {
			t.Fatalf("enabled=%v prevented readiness: %s", enabled, response.Body.String())
		}
	}
}
