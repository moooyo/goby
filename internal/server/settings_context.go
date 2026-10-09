package server

import (
	"context"
	"net/http"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/settings"
	"github.com/moooyo/goby/internal/transcode"
)

type settingsSnapshotContextKey struct{}

// A request carries only committed effective values and their revision. The
// override pointers and deployment configuration never enter this snapshot.
type requestSettingsSnapshot struct {
	Revision            int64
	Effective           settings.Values
	DesiredNetwork      settings.NetworkValues
	TranscodingMaxWidth int
	HardwareSelection   settings.HardwareSelection
	Execution           transcode.ExecutionOptions
	// Direct handler fixtures without managed settings capture their deployment
	// profile here. Production snapshots carry only the opaque selection above.
	hardwareFallback            transcode.Hardware
	hardwareFallbackUnavailable bool
}

func (s *Server) currentSettingsSnapshot() requestSettingsSnapshot {
	if s.settings != nil {
		snapshot := s.settings.ValueSnapshot()
		return requestSettingsSnapshot{Revision: snapshot.Revision, Effective: snapshot.Effective,
			DesiredNetwork: snapshot.DesiredNetwork, TranscodingMaxWidth: snapshot.Encoding.TranscodingMaxWidth,
			HardwareSelection: snapshot.Hardware, Execution: snapshot.Execution}
	}
	// Direct handler fixtures may have no settings store. Production startup
	// initializes the store before its request handler is exposed.
	execution := s.cfg.Transcoding.Execution
	if execution == (transcode.ExecutionOptions{}) {
		execution = transcode.DefaultExecutionOptions(s.cfg.Transcoding.Threads)
	}
	binding, _ := config.HTTPBindingDefaults(s.cfg.ListenAddress)
	return requestSettingsSnapshot{DesiredNetwork: settings.NetworkValues{BindHost: binding.BindHost, HttpPort: binding.HttpPort}, hardwareFallback: s.cfg.Transcoding.Hardware, hardwareFallbackUnavailable: s.cfg.Transcoding.HardwareUnavailable, Execution: execution, Effective: settings.Values{
		ServerName: s.cfg.ServerName, MaxBitrate: s.cfg.Transcoding.MaxBitrate,
		MaxWidth: s.cfg.Transcoding.MaxWidth, MaxHeight: s.cfg.Transcoding.MaxHeight,
		MaxAudioChannels: s.cfg.Transcoding.MaxAudioChannels,
	}}
}

func (s *Server) withSettingsSnapshot(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, exists := r.Context().Value(settingsSnapshotContextKey{}).(requestSettingsSnapshot); !exists {
			snapshot := s.currentSettingsSnapshot()
			r = r.WithContext(context.WithValue(r.Context(), settingsSnapshotContextKey{}, snapshot))
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requestSettings(r *http.Request) requestSettingsSnapshot {
	if snapshot, exists := r.Context().Value(settingsSnapshotContextKey{}).(requestSettingsSnapshot); exists {
		return snapshot
	}
	return s.currentSettingsSnapshot()
}

// New planning receives a private copy of startup execution configuration with
// the native output limits and an optional additional configuration ceiling.
// Removing that additional ceiling cannot relax the native server policy.
// Existing plans retain their concrete output and execution settings.
func (s *Server) requestPlanningConfig(r *http.Request) config.TranscodingConfig {
	snapshot := s.requestSettings(r)
	planning := s.cfg.Transcoding
	planning.MaxBitrate = snapshot.Effective.MaxBitrate
	planning.MaxWidth = snapshot.Effective.MaxWidth
	if snapshot.TranscodingMaxWidth > 0 && snapshot.TranscodingMaxWidth < planning.MaxWidth {
		planning.MaxWidth = snapshot.TranscodingMaxWidth
	}
	planning.MaxHeight = snapshot.Effective.MaxHeight
	planning.MaxAudioChannels = snapshot.Effective.MaxAudioChannels
	hardware, available, _ := s.resolveSettingsHardware(snapshot)
	planning.Hardware = hardware
	planning.HardwareUnavailable = !available
	planning.Execution = snapshot.Execution
	planning.Threads = snapshot.Execution.Threads
	return planning
}

// Hardware consumers resolve the captured selection at use time. A settings
// publication cannot retarget it, and ordinary scalar readers perform no device I/O.
func (s *Server) resolveSettingsHardware(snapshot requestSettingsSnapshot) (transcode.Hardware, bool, string) {
	if snapshot.HardwareSelection != (settings.HardwareSelection{}) {
		return s.resolveManagedHardware(snapshot.HardwareSelection)
	}
	return snapshot.hardwareFallback, !snapshot.hardwareFallbackUnavailable, ""
}

// Direct handler fixtures without the process inventory retain their explicit
// deployment profile. Production always constructs the immutable inventory
// before loading managed settings, and never resolves an administrator path.
func (s *Server) resolveManagedHardware(selection settings.HardwareSelection) (transcode.Hardware, bool, string) {
	if s.managedHardware != nil {
		return s.managedHardware.resolve(selection)
	}
	if selection.Decode == "software" && selection.Encode == "software" && selection.DeviceID == "" {
		return transcode.Hardware{Decode: "software", Encode: "software"}, true, ""
	}
	return transcode.Hardware{Decode: selection.Decode, Encode: selection.Encode}, false, "hardware_inventory_unavailable"
}
