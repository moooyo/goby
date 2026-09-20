package server

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

const (
	maxHardwareEncodingEntries = 128
	hardwareEncodingUsableTTL  = 15 * time.Minute
	hardwareEncodingRejectTTL  = time.Minute
	hardwareEncodingPlanBudget = 6 * time.Second
)

type hardwareEncodingState uint8

const (
	hardwareEncodingUnknown hardwareEncodingState = iota
	hardwareEncodingUsable
	hardwareEncodingRejected
)

type hardwareEncodingKey struct {
	identity string
	request  media.HardwareEncodingRequest
}

type hardwareEncodingEntry struct {
	state   hardwareEncodingState
	code    string
	expires time.Time
	used    time.Time
}

// The one process slot also provides single-flight behavior: a waiter checks
// the bounded cache again after acquiring it. No per-request goroutine, queue,
// GPU process, or unbounded in-flight cache entry is allocated.
type hardwareEncodingRuntime struct {
	mu               sync.Mutex
	entries          map[hardwareEncodingKey]hardwareEncodingEntry
	slot             chan struct{}
	ctx              context.Context
	cancel           context.CancelFunc
	now              func() time.Time
	identify         func(context.Context, string, string, string) (string, error)
	probe            func(context.Context, string, string, media.HardwareEncodingRequest) (media.HardwareEncodingResult, error)
	toolIdentity     func(context.Context, string) (string, error)
	encoders         func(context.Context, string) ([]string, error)
	authorizeDevice  func(string) bool
	softwareIdentity string
	softwareExpires  time.Time
	softwareEncoders map[string]bool
}

func newHardwareEncodingRuntime() *hardwareEncodingRuntime {
	ctx, cancel := context.WithCancel(context.Background())
	return &hardwareEncodingRuntime{entries: make(map[hardwareEncodingKey]hardwareEncodingEntry), slot: make(chan struct{}, 1),
		ctx: ctx, cancel: cancel, now: time.Now, identify: media.HardwareEncodingIdentity, probe: media.ProbeHardwareEncoding,
		toolIdentity: media.EncodingToolIdentity, encoders: media.ProbeVideoEncoders}
}

func (runtime *hardwareEncodingRuntime) softwareAvailable(ctx context.Context, ffmpeg, encoder string, allowProbe bool) bool {
	if ctx.Err() != nil || runtime.ctx.Err() != nil {
		return false
	}
	identity, err := runtime.toolIdentity(ctx, ffmpeg)
	if err != nil {
		return false
	}
	lookup := func() (bool, bool) {
		runtime.mu.Lock()
		defer runtime.mu.Unlock()
		return runtime.softwareEncoders[encoder], runtime.softwareIdentity == identity && runtime.now().Before(runtime.softwareExpires)
	}
	if available, cached := lookup(); cached {
		return available
	}
	if !allowProbe {
		return false
	}
	select {
	case <-ctx.Done():
		return false
	case <-runtime.ctx.Done():
		return false
	case runtime.slot <- struct{}{}:
	}
	defer func() { <-runtime.slot }()
	if ctx.Err() != nil || runtime.ctx.Err() != nil {
		return false
	}
	if available, cached := lookup(); cached {
		return available
	}
	work, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(runtime.ctx, cancel)
	defer stop()
	defer cancel()
	encoders, err := runtime.encoders(work, ffmpeg)
	if err != nil || len(encoders) > 4096 {
		return false
	}
	after, err := runtime.toolIdentity(ctx, ffmpeg)
	if err != nil || after != identity || ctx.Err() != nil || runtime.ctx.Err() != nil {
		return false
	}
	available := make(map[string]bool, 3)
	for _, name := range encoders {
		if name == "libx264" || name == "libx265" || name == "libaom-av1" {
			available[name] = true
		}
	}
	runtime.mu.Lock()
	runtime.softwareIdentity, runtime.softwareExpires, runtime.softwareEncoders = identity, runtime.now().Add(hardwareEncodingUsableTTL), available
	runtime.mu.Unlock()
	return available[encoder]
}

func (s *Server) hardwareEncodingRuntime() *hardwareEncodingRuntime {
	s.hardwareEncodingOnce.Do(func() {
		if s.hardwareEncoding == nil {
			s.hardwareEncoding = newHardwareEncodingRuntime()
		}
		if s.managedHardware != nil {
			s.hardwareEncoding.authorizeDevice = func(device string) bool {
				available, _ := s.managedHardware.checkHardware(transcode.Hardware{Decode: "vaapi", Encode: "vaapi", Device: device})
				return available
			}
		}
	})
	return s.hardwareEncoding
}

func (runtime *hardwareEncodingRuntime) cached(key hardwareEncodingKey) (media.HardwareEncodingResult, bool) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	now := runtime.now()
	entry, exists := runtime.entries[key]
	if !exists || !now.Before(entry.expires) || entry.state == hardwareEncodingUnknown {
		delete(runtime.entries, key)
		return media.HardwareEncodingResult{}, false
	}
	entry.used = now
	runtime.entries[key] = entry
	return media.HardwareEncodingResult{Usable: entry.state == hardwareEncodingUsable, Code: entry.code}, true
}

func (runtime *hardwareEncodingRuntime) remember(key hardwareEncodingKey, result media.HardwareEncodingResult) {
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	now := runtime.now()
	for candidate, entry := range runtime.entries {
		if !now.Before(entry.expires) {
			delete(runtime.entries, candidate)
		}
	}
	if len(runtime.entries) >= maxHardwareEncodingEntries {
		var oldest hardwareEncodingKey
		var used time.Time
		for candidate, entry := range runtime.entries {
			if used.IsZero() || entry.used.Before(used) {
				oldest, used = candidate, entry.used
			}
		}
		delete(runtime.entries, oldest)
	}
	state, ttl := hardwareEncodingRejected, hardwareEncodingRejectTTL
	if result.Usable {
		state, ttl = hardwareEncodingUsable, hardwareEncodingUsableTTL
	}
	runtime.entries[key] = hardwareEncodingEntry{state: state, code: result.Code, expires: now.Add(ttl), used: now}
}

func (runtime *hardwareEncodingRuntime) check(ctx context.Context, ffmpeg, ffprobe string, request media.HardwareEncodingRequest, allowProbe bool) (media.HardwareEncodingResult, error) {
	if err := ctx.Err(); err != nil {
		return media.HardwareEncodingResult{}, err
	}
	if err := runtime.ctx.Err(); err != nil {
		return media.HardwareEncodingResult{}, err
	}
	if runtime.authorizeDevice != nil && !runtime.authorizeDevice(request.Device) {
		return media.HardwareEncodingResult{Code: "hardware_device_unavailable"}, nil
	}
	identity, err := runtime.identify(ctx, ffmpeg, ffprobe, request.Device)
	if ctx.Err() != nil {
		return media.HardwareEncodingResult{}, ctx.Err()
	}
	if runtime.ctx.Err() != nil {
		return media.HardwareEncodingResult{}, runtime.ctx.Err()
	}
	if err != nil {
		return media.HardwareEncodingResult{Code: "hardware_encoding_identity_unavailable"}, nil
	}
	key := hardwareEncodingKey{identity: identity, request: request}
	if result, exists := runtime.cached(key); exists {
		if runtime.authorizeDevice != nil && !runtime.authorizeDevice(request.Device) {
			return media.HardwareEncodingResult{Code: "hardware_device_unavailable"}, nil
		}
		return result, nil
	}
	if !allowProbe {
		return media.HardwareEncodingResult{Code: "hardware_encoding_not_checked"}, nil
	}
	select {
	case <-ctx.Done():
		return media.HardwareEncodingResult{}, ctx.Err()
	case <-runtime.ctx.Done():
		return media.HardwareEncodingResult{}, runtime.ctx.Err()
	case runtime.slot <- struct{}{}:
	}
	defer func() { <-runtime.slot }()
	if err := ctx.Err(); err != nil {
		return media.HardwareEncodingResult{}, err
	}
	if err := runtime.ctx.Err(); err != nil {
		return media.HardwareEncodingResult{}, err
	}
	if runtime.authorizeDevice != nil && !runtime.authorizeDevice(request.Device) {
		return media.HardwareEncodingResult{Code: "hardware_device_unavailable"}, nil
	}
	// The single-flight slot may have been held while executables, libraries
	// or device facts changed. Only evidence for the newly observed identity
	// may be reused after the wait.
	identity, err = runtime.identify(ctx, ffmpeg, ffprobe, request.Device)
	if ctx.Err() != nil {
		return media.HardwareEncodingResult{}, ctx.Err()
	}
	if runtime.ctx.Err() != nil {
		return media.HardwareEncodingResult{}, runtime.ctx.Err()
	}
	if err != nil {
		return media.HardwareEncodingResult{Code: "hardware_encoding_identity_unavailable"}, nil
	}
	key = hardwareEncodingKey{identity: identity, request: request}
	if result, exists := runtime.cached(key); exists {
		if runtime.authorizeDevice != nil && !runtime.authorizeDevice(request.Device) {
			return media.HardwareEncodingResult{Code: "hardware_device_unavailable"}, nil
		}
		return result, nil
	}
	work, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(runtime.ctx, cancel)
	defer stop()
	defer cancel()
	result, err := runtime.probe(work, ffmpeg, ffprobe, request)
	if ctx.Err() != nil {
		return media.HardwareEncodingResult{}, ctx.Err()
	}
	if runtime.ctx.Err() != nil {
		return media.HardwareEncodingResult{}, runtime.ctx.Err()
	}
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return media.HardwareEncodingResult{Code: "hardware_encoding_probe_timeout"}, nil
		}
		result = media.HardwareEncodingResult{Code: "hardware_encoding_probe_failed"}
	}
	after, identityErr := runtime.identify(ctx, ffmpeg, ffprobe, request.Device)
	if ctx.Err() != nil {
		return media.HardwareEncodingResult{}, ctx.Err()
	}
	if identityErr != nil || after != identity || runtime.authorizeDevice != nil && !runtime.authorizeDevice(request.Device) {
		return media.HardwareEncodingResult{Code: "hardware_encoding_identity_changed"}, nil
	}
	runtime.remember(key, result)
	return result, nil
}

// resolveHardwareEncoding is called only after source, request, subtitle and
// playback-reference authorization, before an immutable output revision exists.
// Software fallback changes the encoder implementation, never the client codec,
// profile, bit depth, geometry, bitrate, filtering, or authorization contract.
func (s *Server) resolveHardwareEncoding(ctx context.Context, limits playback.ConversionLimits, decision playback.ConversionDecision, allowProbe bool) (playback.ConversionDecision, error) {
	if err := ctx.Err(); err != nil {
		return decision, err
	}
	if decision.Plan != nil && (limits.HardwareUnavailable && transcode.VideoEncodingSupported(decision.Plan.VideoCodec) || !s.plannedHardwareAvailable(*decision.Plan)) {
		return decision, errHLSRequestUnsupported
	}
	if decision.Plan == nil || !transcode.VideoEncodingSupported(decision.Plan.VideoCodec) || decision.Plan.Hardware.Encode != "vaapi" {
		return decision, nil
	}
	if !limits.AllowVideoTranscode {
		return decision, library.ErrForbidden
	}
	if err := transcode.ValidatePlan(*decision.Plan); err != nil {
		return decision, err
	}
	plan := *decision.Plan
	device := plan.Hardware.Device
	if device == "" {
		device = "/dev/dri/renderD128"
	}
	request := media.HardwareEncodingRequest{Device: device, Codec: plan.VideoCodec, Profile: transcode.VideoOutputProfile(plan),
		BitDepth: transcode.VideoOutputBitDepth(plan), Width: plan.Width, Height: plan.Height, FrameRate: plan.FrameRate, Bitrate: plan.VideoBitrate}
	work, cancel := context.WithTimeout(ctx, hardwareEncodingPlanBudget)
	defer cancel()
	softwareAvailable := s.hardwareEncodingRuntime().softwareAvailable(work, s.cfg.FFmpegPath, transcode.VideoEncoder(plan.VideoCodec, "software"), allowProbe)
	code := ""
	for index := 0; index < max(1, plan.HLS.RenditionCount); index++ {
		if plan.HLS.RenditionCount != 0 {
			request.Width, request.Height = plan.HLS.Renditions[index].Width, plan.HLS.Renditions[index].Height
			request.Bitrate = plan.HLS.Renditions[index].VideoBitrate
		}
		if request.Width == 0 || request.Height == 0 {
			code = "hardware_encoding_dimensions_unknown"
			break
		}
		result, err := s.hardwareEncodingRuntime().check(work, s.cfg.FFmpegPath, s.cfg.FFprobePath, request, allowProbe)
		if ctx.Err() != nil {
			return decision, ctx.Err()
		}
		if err != nil {
			if s.hardwareEncodingRuntime().ctx.Err() != nil {
				return decision, s.hardwareEncodingRuntime().ctx.Err()
			}
			code = "hardware_encoding_probe_budget"
			break
		}
		if !result.Usable {
			code = result.Code
			break
		}
	}
	if ctx.Err() != nil {
		return decision, ctx.Err()
	}
	if s.hardwareEncodingRuntime().ctx.Err() != nil {
		return decision, s.hardwareEncodingRuntime().ctx.Err()
	}
	if code == "hardware_device_unavailable" || !s.plannedHardwareAvailable(plan) {
		return decision, errHLSRequestUnsupported
	}
	if code == "" {
		return decision, nil
	}
	if !softwareAvailable {
		if s.log != nil {
			s.log.WarnContext(ctx, "hardware encoder fallback unavailable", "code", "hardware_encoding_fallback_unavailable", "hardware_code", code,
				"codec", request.Codec, "profile", request.Profile, "bit_depth", request.BitDepth, "width", request.Width, "height", request.Height)
		}
		return decision, errHLSRequestUnsupported
	}
	plan = softwareEncodingPlan(plan)
	if limits.Execution != (transcode.ExecutionOptions{}) {
		captured, err := transcode.CaptureExecution(plan, limits.Execution)
		if err != nil {
			return decision, errHLSRequestUnsupported
		}
		plan = captured
	}
	if err := transcode.ValidatePlan(plan); err != nil {
		return decision, err
	}
	decision, err := playback.ReprojectVideoEncodingOutput(decision, plan)
	if err != nil {
		return decision, err
	}
	if decision.Plan == nil {
		return decision, errHLSRequestUnsupported
	}
	decision.Reasons = append(append([]playback.Reason(nil), decision.Reasons...), playback.Reason{Code: code, Property: "HardwareEncoding",
		Message: "The exact hardware output tuple was not admitted; the authorized software encoder preserves the requested output format."})
	if s.log != nil {
		s.log.InfoContext(ctx, "hardware encoder fallback", "code", code, "codec", request.Codec, "profile", request.Profile,
			"bit_depth", request.BitDepth, "width", request.Width, "height", request.Height)
	}
	return decision, nil
}

// Physical identity is distinct from both the requested policy and an exact
// cached encoder proof. A missing or replaced approved node cannot take the
// ordinary failed-tuple fallback route or silently select the default node.
func (s *Server) plannedHardwareAvailable(plan transcode.Plan) bool {
	if plan.VideoFilters.Backend == "vulkan" && plan.Hardware.Device == "" {
		return false
	}
	if !planUsesHardware(plan) || s.managedHardware == nil {
		return true
	}
	available, _ := s.managedHardware.checkHardware(plan.Hardware)
	return available
}

func planUsesHardware(plan transcode.Plan) bool {
	if plan.VideoCodec == "" || plan.VideoCodec == "copy" {
		return false
	}
	return plan.Hardware.Decode != "" && plan.Hardware.Decode != "software" ||
		plan.Hardware.Encode != "" && plan.Hardware.Encode != "software" ||
		plan.VideoFilters.Backend == "vaapi" || plan.VideoFilters.Backend == "vulkan"
}

func softwareEncodingPlan(plan transcode.Plan) transcode.Plan {
	plan.Hardware.Encode = "software"
	if plan.Hardware.Decode == "" || plan.Hardware.Decode == "software" {
		if plan.VideoFilters.Backend == "vaapi" {
			// VAAPI VPP-only plans need a device-bearing codec stage in the
			// current runner. Preserve the operation through its CPU equivalent.
			plan.VideoFilters.Backend = ""
		}
		if plan.VideoFilters.Backend != "vulkan" {
			plan.Hardware.Device = ""
		}
	}
	return plan
}

// Existing revisions retain their admitted implementation. Reauthorization
// still replans all media facts and permissions, but neither probes again nor
// promotes a software revision if a previously rejected GPU tuple later works.
func hardwareEncodingRevisionMatches(candidate *transcode.Plan, revision transcode.Plan) bool {
	if candidate == nil {
		return false
	}
	if *candidate == revision {
		return true
	}
	return candidate.Hardware.Encode == "vaapi" && revision.Hardware.Encode == "software" && softwareEncodingPlan(*candidate) == revision
}
