package server

import (
	"net/http"

	"github.com/moooyo/goby/internal/systemstatus"
	"github.com/moooyo/goby/internal/transcode"
)

type systemStatusReader interface {
	Snapshot() systemstatus.Snapshot
}

type adminTranscodingMetrics struct {
	Available      bool
	Active         *int
	Limit          *int
	HardwareActive *int
	SoftwareActive *int
}

type adminSystemStatusDTO struct {
	systemstatus.Snapshot
	Transcoding adminTranscodingMetrics
}

func (s *Server) adminSystemStatus(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		apiError(w, r, http.StatusBadRequest, "invalid_query", "System status does not accept query parameters.")
		return
	}
	if s.hostStatus == nil {
		apiError(w, r, http.StatusServiceUnavailable, "system_status_unavailable", "System status is unavailable.")
		return
	}
	jsonResponse(w, http.StatusOK, adminSystemStatusDTO{Snapshot: s.hostStatus.Snapshot(), Transcoding: s.adminTranscodingMetrics()})
}

func (s *Server) adminTranscodingMetrics() adminTranscodingMetrics {
	if s.hls == nil {
		if s.cfg.Transcoding.Enabled {
			return adminTranscodingMetrics{}
		}
		// A disabled engine has no conversion jobs or usable conversion slots.
		zero := 0
		return adminTranscodingMetrics{Active: &zero, Limit: &zero, HardwareActive: &zero, SoftwareActive: &zero}
	}
	s.hls.mu.Lock()
	manager := s.hls.manager
	s.hls.mu.Unlock()
	reporter, ok := manager.(interface{ Metrics() transcode.Metrics })
	if !ok {
		return adminTranscodingMetrics{}
	}
	metrics := reporter.Metrics()
	if metrics.Running < 0 || metrics.Hardware < 0 || metrics.Software < 0 || metrics.MaxJobs <= 0 ||
		metrics.Running > metrics.MaxJobs || metrics.Running-metrics.Hardware != metrics.Software {
		return adminTranscodingMetrics{}
	}
	return adminTranscodingMetrics{Available: true, Active: &metrics.Running, Limit: &metrics.MaxJobs,
		HardwareActive: &metrics.Hardware, SoftwareActive: &metrics.Software}
}
