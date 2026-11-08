package server

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/settings"
)

const compiledCodecLifetime = 15 * time.Minute

var errCodecInventoryBusy = errors.New("codec inventory is busy")

type compiledCodecSnapshot struct {
	value    media.Capabilities
	observed time.Time
	state    string
}

type adminCodecInterface struct {
	Managed         bool
	Compiled        *bool
	Selected        bool
	Implementations []string
}

type adminCodecProof struct {
	DeviceId  string
	Codec     string
	Profile   string
	BitDepth  int
	Width     int
	Height    int
	FrameRate float64
	Bitrate   int64
	State     string
	Code      string
	ExpiresAt time.Time
}

type adminCodecCapability struct {
	Codec          string
	Name           string
	SoftwareDecode adminCodecInterface
	SoftwareEncode adminCodecInterface
	HardwareDecode adminCodecInterface
	HardwareEncode adminCodecInterface
	EncodingProofs []adminCodecProof
}

type adminCodecDevice struct {
	DeviceId  string
	Label     string
	Available bool
	Code      string
}

type adminCodecSelection struct {
	Decode   string
	Encode   string
	DeviceId string
}

type adminCodecCapabilities struct {
	ObservedAt           time.Time
	CompiledAt           *time.Time
	CompiledState        string
	FFmpegVersion        string
	TranscodingAvailable bool
	Selected             adminCodecSelection
	Devices              []adminCodecDevice
	Items                []adminCodecCapability
}

type managedCodecDefinition struct {
	id, name string
	decoders []string
	encoder  string
}

var managedCodecDefinitions = []managedCodecDefinition{
	{"h264", "H.264 / AVC", []string{"h264"}, "libx264"},
	{"hevc", "H.265 / HEVC", []string{"hevc"}, "libx265"},
	{"av1", "AV1", []string{"av1", "libdav1d", "libaom-av1"}, "libaom-av1"},
	{"vp9", "VP9", []string{"vp9"}, ""},
	{"mpeg2video", "MPEG-2", []string{"mpeg2video"}, ""},
	{"mpeg4", "MPEG-4 Part 2", []string{"mpeg4"}, ""},
	{"vc1", "VC-1", []string{"vc1"}, ""},
}

func (s *Server) registerAdminCodecCapabilityRoutes(mux *http.ServeMux) {
	wrap := func(handler http.HandlerFunc) http.HandlerFunc {
		authorized := s.requireAdmin(handler)
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Pragma", "no-cache")
			authorized(w, r)
		}
	}
	mux.HandleFunc("GET /admin/v1/hardware/codecs", wrap(s.adminCodecCapabilities))
	mux.HandleFunc("POST /admin/v1/hardware/codecs/refresh", wrap(s.refreshAdminCodecCapabilities))
}

func (s *Server) adminCodecCapabilities(w http.ResponseWriter, r *http.Request) {
	if !mediaDiagnosticRequest(w, r) {
		return
	}
	s.writeAdminCodecCapabilities(w, r)
}

func (s *Server) refreshAdminCodecCapabilities(w http.ResponseWriter, r *http.Request) {
	if _, ok := mediaDiagnosticBody(w, r, []string{}); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	err := s.hardwareEncodingRuntime().refreshCompiledCodecs(ctx, s.cfg.FFmpegPath)
	if errors.Is(err, errCodecInventoryBusy) {
		apiError(w, r, http.StatusConflict, "codec_inventory_busy", "Media capability information is already being refreshed. Wait for the active operation to finish.")
		return
	}
	if err != nil {
		apiError(w, r, http.StatusServiceUnavailable, "codec_inventory_unavailable", "The configured FFmpeg capability information could not be read. Check the server tool configuration and try again.")
		return
	}
	actor := r.Context().Value(principalKey).(identity.Principal)
	if err := identity.CheckAdministratorRead(ctx, s.db, actor, identity.AdministratorNative); err != nil {
		s.identityError(w, r, err)
		return
	}
	s.writeAdminCodecCapabilities(w, r.WithContext(ctx))
}

func (s *Server) writeAdminCodecCapabilities(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	snapshot := s.requestSettings(r)
	devices, _, _ := s.managedHardware.status(snapshot.HardwareSelection)
	runtime := s.hardwareEncodingRuntime()
	compiled := runtime.compiledCodecs(ctx, s.cfg.FFmpegPath)
	proofs := s.currentCodecEncodingProofs(ctx, runtime, devices)
	if err := ctx.Err(); err != nil {
		apiError(w, r, http.StatusServiceUnavailable, "codec_inventory_unavailable", "The current codec information could not be observed within its time limit.")
		return
	}
	result := projectAdminCodecCapabilities(compiled, snapshot.HardwareSelection, devices, s.transcodingStatus().Available, proofs, runtime.now())
	jsonResponse(w, http.StatusOK, result)
}

func copyCompiledCodecs(value media.Capabilities) media.Capabilities {
	value.HardwareAccelerators = append([]string(nil), value.HardwareAccelerators...)
	value.Decoders = append([]string(nil), value.Decoders...)
	value.Encoders = append([]string(nil), value.Encoders...)
	value.Filters = append([]string(nil), value.Filters...)
	value.HardwareVerified = false
	return value
}

// Reading inventory never spawns FFmpeg or opens a GPU. Cached names remain
// separate from runtime execution evidence and expire with the tool identity.
func (runtime *hardwareEncodingRuntime) compiledCodecs(ctx context.Context, ffmpeg string) compiledCodecSnapshot {
	identity, identityErr := runtime.toolIdentity(ctx, ffmpeg)
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	result := compiledCodecSnapshot{value: copyCompiledCodecs(runtime.compiled), observed: runtime.compiledObserved, state: "unknown"}
	if result.observed.IsZero() {
		return result
	}
	result.state = "stale"
	if identityErr == nil && identity == runtime.compiledIdentity && runtime.now().Before(result.observed.Add(compiledCodecLifetime)) {
		result.state = "current"
	}
	return result
}

// Refresh shares the existing finite probe slot. It only enumerates compiled
// interfaces; it neither claims successful decoding nor starts a GPU sample.
func (runtime *hardwareEncodingRuntime) refreshCompiledCodecs(ctx context.Context, ffmpeg string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := runtime.ctx.Err(); err != nil {
		return err
	}
	select {
	case <-runtime.ctx.Done():
		return runtime.ctx.Err()
	case runtime.slot <- struct{}{}:
	default:
		return errCodecInventoryBusy
	}
	defer func() { <-runtime.slot }()
	work, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(runtime.ctx, cancel)
	defer stop()
	defer cancel()
	before, err := runtime.toolIdentity(work, ffmpeg)
	if err != nil {
		return err
	}
	if before == "" {
		return errors.New("encoding tool identity is unavailable")
	}
	if err := work.Err(); err != nil {
		return err
	}
	probe := runtime.capabilityProbe
	if probe == nil {
		probe = media.ProbeCapabilities
	}
	value, err := probe(work, ffmpeg)
	if err != nil {
		return err
	}
	after, err := runtime.toolIdentity(work, ffmpeg)
	if err != nil || after != before {
		return errors.New("encoding tool changed during capability observation")
	}
	if err := work.Err(); err != nil {
		return err
	}
	runtime.mu.Lock()
	runtime.compiled, runtime.compiledIdentity, runtime.compiledObserved = copyCompiledCodecs(value), after, runtime.now()
	runtime.mu.Unlock()
	return nil
}

func codecNameSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func codecInterfaces(compiled compiledCodecSnapshot, managed, selected bool, names []string, available map[string]bool, prerequisite bool) adminCodecInterface {
	result := adminCodecInterface{Managed: managed, Selected: selected && managed, Implementations: append([]string{}, names...)}
	if compiled.state == "current" {
		present := false
		for _, name := range names {
			present = present || available[name]
		}
		present = present && prerequisite
		result.Compiled = &present
	}
	return result
}

func projectAdminCodecCapabilities(compiled compiledCodecSnapshot, selection settings.HardwareSelection, devices []managedHardwareDevice, enabled bool, proofs []adminCodecProof, now time.Time) adminCodecCapabilities {
	result := adminCodecCapabilities{ObservedAt: now.UTC(), CompiledState: compiled.state, FFmpegVersion: compiled.value.Version,
		TranscodingAvailable: enabled, Selected: adminCodecSelection{Decode: selection.Decode, Encode: selection.Encode, DeviceId: selection.DeviceID},
		Devices: make([]adminCodecDevice, 0, len(devices)), Items: make([]adminCodecCapability, 0, len(managedCodecDefinitions))}
	if !compiled.observed.IsZero() {
		observed := compiled.observed.UTC()
		result.CompiledAt = &observed
	}
	for _, device := range devices {
		result.Devices = append(result.Devices, adminCodecDevice{DeviceId: device.DeviceID, Label: device.Label, Available: device.Available, Code: device.Code})
	}
	decoders, encoders, accelerators := codecNameSet(compiled.value.Decoders), codecNameSet(compiled.value.Encoders), codecNameSet(compiled.value.HardwareAccelerators)
	for _, definition := range managedCodecDefinitions {
		softwareEncoders, hardwareEncoders := []string{}, []string{}
		if definition.encoder != "" {
			softwareEncoders, hardwareEncoders = []string{definition.encoder}, []string{definition.id + "_vaapi"}
		}
		item := adminCodecCapability{Codec: definition.id, Name: definition.name,
			SoftwareDecode: codecInterfaces(compiled, true, selection.Decode == "software" || selection.Decode == "", definition.decoders, decoders, true),
			SoftwareEncode: codecInterfaces(compiled, definition.encoder != "", selection.Encode == "software" || selection.Encode == "", softwareEncoders, encoders, true),
			HardwareDecode: codecInterfaces(compiled, true, selection.Decode == "vaapi", definition.decoders, decoders, accelerators["vaapi"]),
			HardwareEncode: codecInterfaces(compiled, definition.encoder != "", selection.Encode == "vaapi", hardwareEncoders, encoders, true),
			EncodingProofs: make([]adminCodecProof, 0)}
		for _, proof := range proofs {
			if proof.Codec == definition.id {
				item.EncodingProofs = append(item.EncodingProofs, proof)
			}
		}
		result.Items = append(result.Items, item)
	}
	return result
}

func (s *Server) currentCodecEncodingProofs(ctx context.Context, runtime *hardwareEncodingRuntime, devices []managedHardwareDevice) []adminCodecProof {
	proofs := make([]adminCodecProof, 0)
	if s.managedHardware == nil {
		return proofs
	}
	identities := make(map[string]string)
	ids := make(map[string]string)
	for _, entry := range s.managedHardware.entries {
		for _, device := range devices {
			if device.DeviceID != entry.id || !device.Available {
				continue
			}
			identity, err := runtime.identify(ctx, s.cfg.FFmpegPath, s.cfg.FFprobePath, entry.path)
			if err == nil {
				identities[entry.path], ids[entry.path] = identity, entry.id
			}
		}
	}
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	now := runtime.now()
	for key, entry := range runtime.entries {
		if identities[key.request.Device] == "" || identities[key.request.Device] != key.identity || !now.Before(entry.expires) || entry.state == hardwareEncodingUnknown {
			continue
		}
		state := "failed"
		if entry.state == hardwareEncodingUsable {
			state = "passed"
		}
		request := key.request
		proofs = append(proofs, adminCodecProof{DeviceId: ids[request.Device], Codec: request.Codec, Profile: request.Profile, BitDepth: request.BitDepth,
			Width: request.Width, Height: request.Height, FrameRate: request.FrameRate, Bitrate: request.Bitrate, State: state, Code: entry.code, ExpiresAt: entry.expires.UTC()})
	}
	sort.Slice(proofs, func(i, j int) bool {
		first, second := proofs[i], proofs[j]
		if first.Codec != second.Codec {
			return first.Codec < second.Codec
		}
		if first.DeviceId != second.DeviceId {
			return first.DeviceId < second.DeviceId
		}
		if first.Profile != second.Profile {
			return first.Profile < second.Profile
		}
		if first.BitDepth != second.BitDepth {
			return first.BitDepth < second.BitDepth
		}
		if first.Width != second.Width {
			return first.Width < second.Width
		}
		if first.Height != second.Height {
			return first.Height < second.Height
		}
		if first.FrameRate != second.FrameRate {
			return first.FrameRate < second.FrameRate
		}
		if first.Bitrate != second.Bitrate {
			return first.Bitrate < second.Bitrate
		}
		return first.ExpiresAt.Before(second.ExpiresAt)
	})
	return proofs
}
