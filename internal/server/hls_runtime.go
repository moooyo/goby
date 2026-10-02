package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

const (
	maxHLSSessions        = 128
	maxHLSUserSessions    = 32
	maxHLSAuthSessions    = 16
	maxHLSProducers       = 8
	hlsProducerSpan       = 16
	hlsIdleTTL            = 5 * time.Minute
	hlsMaintenanceWorkers = 4
)

type hlsKey struct {
	scope  transcode.Scope
	stamp  string
	plan   transcode.Plan
	remote bool
}

type hlsProducer struct {
	id          string
	first, last int
	ownership   *hlsProducerOwnership
}

type hlsJobs interface {
	Ensure(context.Context, transcode.Spec, *os.File) (transcode.Record, error)
	TryOpen(transcode.Scope, string, string) (*transcode.ReadHandle, error)
	Snapshot(transcode.Scope, string) (transcode.Record, error)
	Open(context.Context, transcode.Scope, string, string) (*transcode.ReadHandle, error)
	CancelJob(string, transcode.Scope) error
	Close(context.Context) error
}

// health is separate from the media lookup interface so adapters that cannot
// report engine health are explicitly unavailable rather than assumed healthy.
func (h *hlsRuntime) health() transcode.Health {
	h.mu.Lock()
	closing, manager := h.closing, h.manager
	h.mu.Unlock()
	if closing {
		return transcode.Health{Code: "manager_closed"}
	}
	reporter, ok := manager.(interface{ Health() transcode.Health })
	if !ok {
		return transcode.Health{Code: "engine_status_unavailable"}
	}
	return reporter.Health()
}

type hlsSession struct {
	mu                     sync.Mutex
	id                     string
	key                    hlsKey
	principal              identity.Principal
	output                 playback.Source
	subtitleSource         playback.Source
	subtitleView           playback.HLSSubtitleView
	audioTiming            *media.AudioTiming
	startHint              int64
	accessed               time.Time
	presenceUpdated        time.Time
	maintenanceChecked     time.Time
	closed                 bool
	timeline               *transcode.Timeline
	lead                   int64
	building               chan struct{}
	subtitleProducerClocks map[string]*hlsProducerSubtitleClock
	windowGraph            *hlsGeneratedWindowGraph
	producers              []hlsProducer
	admission              *hlsAdmission
	admissionRevision      uint64
	progressiveReaders     int
	lastAsked              int
	demand                 hlsPlaybackDemand
	ctx                    context.Context
	cancel                 context.CancelFunc
	playbackReference      *playbackAdmissionReference
}

// HLS session IDs identify immutable output revisions, not credentials. Their
// timelines and segment numbers span the complete source, independently of a
// producer that starts later after a seek. Every HTTP use is authorized again.
type hlsRuntime struct {
	server                   *Server
	manager                  hlsJobs
	verify                   func(context.Context, identity.Principal, transcode.Scope, string, transcode.Plan) (*os.File, library.MediaFile, error)
	ctx                      context.Context
	cancel                   context.CancelFunc
	mu                       sync.Mutex
	sessions                 map[string]*hlsSession
	byKey                    map[hlsKey]*hlsSession
	byScope                  map[transcode.Scope]map[string]*hlsSession
	closing                  bool
	admissions               int
	admissionGates           map[hlsAdmissionKey]*hlsAdmissionGate
	producerMu               sync.Mutex
	producerOwners           map[hlsProducerKey]int
	producerDemands          map[hlsProducerKey]int
	generatedWindowPinMu     sync.Mutex
	generatedWindowPinOwners map[*transcode.ReadHandle]*hlsSession
	initializationBudget     hlsInitializationBudget
	requests                 sync.WaitGroup
	workers                  sync.WaitGroup
	once                     sync.Once
	done                     chan struct{}
	closeErr                 error
	probes                   chan struct{}
	slots                    chan struct{}
	// Enabled after the closed-window HTTP/client acceptance matrix passes.
	generatedWindowsEnabled bool
}

func newHLSRuntime(ctx context.Context, server *Server) (*hlsRuntime, error) {
	if !server.cfg.Transcoding.Enabled {
		return nil, nil
	}
	if _, err := exec.LookPath(server.cfg.FFmpegPath); err != nil {
		return nil, errors.New("the configured FFmpeg executable is unavailable")
	}
	if _, err := exec.LookPath(server.cfg.FFprobePath); err != nil {
		return nil, errors.New("the configured FFprobe executable is unavailable")
	}
	managerOptions := server.cfg.Transcoding.ManagerOptions(server.cfg.FFmpegPath, transcode.NewRepository(server.db))
	managerOptions.ValidateHardware = func(ctx context.Context, plan transcode.Plan) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !server.plannedHardwareAvailable(plan) {
			return errHLSRequestUnsupported
		}
		return ctx.Err()
	}
	managerOptions.SubtitleSource = server.readBurnSubtitleAsset
	managerOptions.LivePublish = server.publishDynamicSegment
	managerOptions.LiveSubtitle = server.receiveDynamicSubtitles
	managerOptions.LiveCaption = server.receiveDynamicCaption
	managerOptions.PlaybackAdmission = server.holdCorrelatedHLSAdmission
	managerOptions.PlaybackStopped = server.correlatedHLSPlaybackStopped
	manager, err := transcode.NewManager(ctx, managerOptions)
	if err != nil {
		return nil, err
	}
	lifetime, cancel := context.WithCancel(context.Background())
	runtime := &hlsRuntime{server: server, manager: manager, ctx: lifetime, cancel: cancel, sessions: make(map[string]*hlsSession),
		byKey: make(map[hlsKey]*hlsSession), byScope: make(map[transcode.Scope]map[string]*hlsSession),
		done: make(chan struct{}), probes: make(chan struct{}, 2), slots: make(chan struct{}, 32)}
	runtime.verify = server.authorizeHLS
	runtime.workers.Add(1)
	go runtime.maintain()
	return runtime, nil
}

func (h *hlsRuntime) enter() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing {
		return false
	}
	h.requests.Add(1)
	return true
}

func (h *hlsRuntime) register(principal identity.Principal, source library.MediaFile, playID string, decision playback.ConversionDecision, start int64) (*hlsSession, error) {
	session, _, err := h.registerWithStatus(principal, source, playID, decision, start)
	return session, err
}

func (h *hlsRuntime) registerWithStatus(principal identity.Principal, source library.MediaFile, playID string, decision playback.ConversionDecision, start int64) (*hlsSession, bool, error) {
	return h.registerWithPlaybackOwner(principal, source, playID, decision, start, nil)
}

func (h *hlsRuntime) registerWithPlaybackOwner(principal identity.Principal, source library.MediaFile, playID string, decision playback.ConversionDecision, start int64, parent *playbackAdmissionReference) (*hlsSession, bool, error) {
	if decision.Plan == nil {
		return nil, false, transcode.ErrInvalidPlan
	}
	if !principalPlanBitrateAllowed(principal, source, *decision.Plan) {
		return nil, false, library.ErrForbidden
	}
	plan := *decision.Plan
	if plan.OutputMode == "" {
		plan.StartTicks = 0
	}
	key := hlsKey{scope: transcode.Scope{UserID: principal.User.ID, AuthSessionID: principal.SessionID, DeviceID: principal.Client.DeviceID,
		ApplicationKey: principal.IsApplicationKey(), ApplicationClientID: principal.ClientSessionID,
		PlaySessionID: playID, ItemID: source.Item.ID, SourceID: source.SourceID}, stamp: source.ETag, plan: plan,
		remote: !identity.IsLocalPeer(principal.PeerIP)}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing {
		return nil, false, transcode.ErrManagerClosed
	}
	owned := h.usesPlaybackOwnership(plan)
	if owned && (parent == nil || parent.gate != &h.server.playbackStopIntents) {
		return nil, false, transcode.ErrInvalidScope
	}
	if owned {
		if err := requireLivePlaybackReference(parent, key.scope); err != nil {
			return nil, false, err
		}
	}
	if prior := h.byKey[key]; prior != nil {
		prior.mu.Lock()
		closed := prior.closed
		if !closed {
			prior.accessed = time.Now()
		}
		prior.mu.Unlock()
		if !closed {
			if owned && prior.playbackReference == nil {
				return nil, false, transcode.ErrInvalidScope
			}
			return prior, false, nil
		}
	}
	userCount, authCount := 0, 0
	for _, prior := range h.sessions {
		if !key.scope.ApplicationKey && !prior.key.scope.ApplicationKey && prior.key.scope.UserID == key.scope.UserID {
			userCount++
		}
		if prior.key.scope.AuthSessionID == key.scope.AuthSessionID {
			authCount++
		}
	}
	if len(h.sessions) >= maxHLSSessions || userCount >= maxHLSUserSessions || authCount >= maxHLSAuthSessions {
		return nil, false, transcode.ErrBusy
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, false, err
	}
	var playbackReference *playbackAdmissionReference
	if owned {
		var err error
		playbackReference, err = parent.fork(key.scope)
		if err != nil {
			return nil, false, err
		}
	}
	published := false
	defer func() {
		if !published {
			playbackReference.release()
		}
	}()
	ctx, cancel := context.WithCancel(h.ctx)
	session := &hlsSession{id: hex.EncodeToString(random[:]), key: key, principal: principal, output: decision.OutputSource,
		subtitleSource: playback.Source{ItemID: source.Item.ID, MediaSourceID: source.SourceID, ItemType: source.Item.Type, Info: playbackMediaInfo(source.Item)},
		subtitleView:   decision.SubtitleView,
		startHint:      start, accessed: time.Now(), presenceUpdated: time.Now(), lastAsked: -1, ctx: ctx, cancel: cancel,
		playbackReference: playbackReference}
	if !session.subtitleView.SelectionSet {
		if plan.Subtitle.Mode == "hls" {
			session.subtitleView.OffsetTicks = plan.Subtitle.OffsetTicks
		}
		view, err := playback.HLSSubtitleViewFor(plan, nil, session.subtitleView.OffsetTicks)
		if err != nil {
			cancel()
			return nil, false, errHLSRequestInvalid
		}
		session.subtitleView = view
	}
	if source.Item.Type == "Audio" && source.Item.Media != nil {
		if timing := exactAudioCoverage(*source.Item.Media, plan.AudioStreamIndex); timing != nil {
			copy := *timing
			session.audioTiming = &copy
		}
	}
	h.sessions[session.id], h.byKey[key] = session, session
	if h.byScope == nil {
		h.byScope = make(map[transcode.Scope]map[string]*hlsSession)
	}
	if h.byScope[key.scope] == nil {
		h.byScope[key.scope] = make(map[string]*hlsSession)
	}
	h.byScope[key.scope][session.id] = session
	published = true
	return session, true, nil
}

func (h *hlsRuntime) find(id string, principal identity.Principal, itemID string) (*hlsSession, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	session := h.sessions[id]
	if h.closing || session == nil || session.key.scope.ApplicationKey != principal.IsApplicationKey() || session.key.scope.UserID != principal.User.ID ||
		session.key.scope.ApplicationClientID != principal.ClientSessionID ||
		session.key.scope.AuthSessionID != principal.SessionID || session.key.scope.DeviceID != principal.Client.DeviceID ||
		session.key.scope.ItemID != itemID || session.key.remote != !identity.IsLocalPeer(principal.PeerIP) {
		return nil, transcode.ErrJobNotFound
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return nil, transcode.ErrJobNotFound
	}
	session.accessed = time.Now()
	return session, nil
}

func (h *hlsRuntime) retire(session *hlsSession) {
	h.mu.Lock()
	session.mu.Lock()
	session.closed = true
	if session.windowGraph != nil {
		session.windowGraph.initialization.release()
	}
	session.cancel()
	if session.admission != nil {
		session.admission.cancel()
	}
	h.releaseGeneratedWindowPinsLocked(session)
	// CancelJob immediately fences manager deduplication without waiting for
	// process exit. Complete that fence before the same registry key can be
	// registered again, or a replacement could inherit the retiring producer.
	for _, producer := range session.producers {
		h.releaseProducer(session.key.scope, producer)
	}
	// Pending workers and source loans retain their independent descendants.
	// Releasing the registry parent never stands in for their actual join.
	session.playbackReference.release()
	session.playbackReference = nil
	if h.sessions[session.id] == session {
		delete(h.sessions, session.id)
		if registrations := h.byScope[session.key.scope]; registrations != nil {
			delete(registrations, session.id)
			if len(registrations) == 0 {
				delete(h.byScope, session.key.scope)
			}
		}
		if h.byKey[session.key] == session {
			delete(h.byKey, session.key)
		}
	}
	session.mu.Unlock()
	h.mu.Unlock()
}

func (h *hlsRuntime) cancelMatching(authID, playID string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	var matching []*hlsSession
	for _, session := range h.sessions {
		if session.key.scope.AuthSessionID == authID && (playID == "" || session.key.scope.PlaySessionID == playID) {
			matching = append(matching, session)
		}
	}
	h.mu.Unlock()
	for _, session := range matching {
		h.retire(session)
	}
}

// cancelCredential also invalidates a producer whose HLS registration has
// already retired. The manager closes its process asynchronously.
func (h *hlsRuntime) cancelCredential(authID string) {
	if h == nil || authID == "" {
		return
	}
	h.cancelMatching(authID, "")
	h.mu.Lock()
	manager := h.manager
	h.mu.Unlock()
	if scoped, ok := manager.(interface{ CancelSession(string) }); ok {
		scoped.CancelSession(authID)
	}
}

// cancelPlayback also reaches jobs whose output registration has already been
// removed. A stopped play must fence every producer before another lookup.
func (h *hlsRuntime) cancelPlayback(authID, playID string) {
	if h == nil || authID == "" || playID == "" {
		return
	}
	h.cancelMatching(authID, playID)
	h.mu.Lock()
	manager := h.manager
	h.mu.Unlock()
	if scoped, ok := manager.(interface{ CancelPlayback(string, string) }); ok {
		scoped.CancelPlayback(authID, playID)
	}
}

// cancelFileHLSPlayback belongs only to the early correlated file-HLS intent.
// Shared media policy and other transports retain post-commit cancellation.
// The manager sweep also reaches file-HLS jobs without an attached registry.
func (h *hlsRuntime) cancelFileHLSPlayback(authID, playID string) error {
	if h == nil || authID == "" || playID == "" {
		return library.ErrUnavailable
	}
	h.mu.Lock()
	var matching []*hlsSession
	for _, session := range h.sessions {
		if session.key.scope.AuthSessionID == authID && session.key.scope.PlaySessionID == playID && correlatedFileHLSPlan(session.key.plan) {
			matching = append(matching, session)
		}
	}
	manager := h.manager
	h.mu.Unlock()
	for _, session := range matching {
		h.retire(session)
	}
	if scoped, ok := manager.(interface{ CancelFileHLSPlayback(string, string) }); ok {
		scoped.CancelFileHLSPlayback(authID, playID)
		return nil
	}
	// No generic fallback may retire an unguarded progressive/dynamic input.
	return library.ErrUnavailable
}

func (h *hlsRuntime) touchMatching(authID, playID string) {
	if h == nil || playID == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, session := range h.sessions {
		if session.key.scope.AuthSessionID == authID && session.key.scope.PlaySessionID == playID {
			session.mu.Lock()
			if !session.closed {
				session.accessed = time.Now()
			}
			session.mu.Unlock()
		}
	}
}

func permanentHLSError(err error) bool {
	return errors.Is(err, identity.ErrUnauthorized) || errors.Is(err, library.ErrForbidden) || errors.Is(err, library.ErrNotFound) || errors.Is(err, library.ErrSourceChanged)
}

func (s *Server) authorizeHLS(ctx context.Context, principal identity.Principal, scope transcode.Scope, stamp string, plan transcode.Plan) (resultFile *os.File, resultSource library.MediaFile, resultErr error) {
	owned := principal.SessionID == scope.AuthSessionID && principal.User.ID == scope.UserID && principal.ClientSessionID == scope.ApplicationClientID &&
		principal.Client.DeviceID == scope.DeviceID && principal.IsApplicationKey() == scope.ApplicationKey
	defer func() {
		if owned && permanentHLSError(resultErr) && !s.correlatedHLSPlaybackStopped(transcode.Spec{Scope: scope, Plan: plan}) {
			s.cancelMediaPolicy(scope.AuthSessionID, scope.PlaySessionID)
		}
	}()
	file, authorization, err := s.library.AuthorizePlaybackMediaForChecked(ctx, principal, scope.PlaySessionID,
		scope.ItemID, scope.SourceID, transcode.PlanHLSSubtitles(plan).Count > 0, func(current library.PlaybackMediaAuthorization) error {
			return s.checkHLSPlaybackAuthorization(current, scope, plan)
		})
	if err != nil {
		return nil, library.MediaFile{}, err
	}
	fresh, source := authorization.Principal, authorization.Source
	defer func() {
		if resultErr != nil {
			_ = file.Close()
		}
	}()
	if !time.Now().Before(authorization.Play.ExpiresAt) {
		return nil, library.MediaFile{}, library.ErrNotFound
	}
	if stamp != "" && stamp != source.ETag {
		return nil, library.MediaFile{}, library.ErrNotFound
	}
	if !principalPlanBitrateAllowed(fresh, source, plan) {
		return nil, library.MediaFile{}, library.ErrForbidden
	}
	if plan.Subtitle.ExternalTag != "" {
		if _, err := s.readPlannedExternalSubtitle(ctx, fresh, scope, plan); err != nil {
			return nil, library.MediaFile{}, err
		}
	}
	if err := s.authorizeHLSSubtitles(ctx, fresh, scope, source, plan); err != nil {
		return nil, library.MediaFile{}, err
	}
	return file, source, nil
}

// Application credentials use the server's execution limits without borrowing
// a user's playback policy. Login sessions continue to enforce persisted policy.
func hlsPrincipalLimits(cfg config.TranscodingConfig, principal identity.Principal) playback.ConversionLimits {
	if !principal.IsApplicationKey() {
		return applyPrincipalRemoteBitrateLimit(hlsUserLimits(cfg, principal.User), principal)
	}
	return hlsServerLimits(cfg)
}

func hlsServerLimits(cfg config.TranscodingConfig) playback.ConversionLimits {
	return playback.ConversionLimits{
		MaxBitrate: cfg.MaxBitrate, MaxWidth: cfg.MaxWidth, MaxHeight: cfg.MaxHeight,
		MaxAudioChannels: cfg.MaxAudioChannels, Hardware: cfg.Hardware, Execution: cfg.Execution,
		HardwareUnavailable: cfg.HardwareUnavailable,
		AllowRemux:          cfg.Enabled, AllowAudioTranscode: cfg.Enabled, AllowVideoTranscode: cfg.Enabled,
	}
}

// An application key's explicit profile target supplies conversion permissions,
// not account authentication. Disabled accounts and the general playback flag
// do not change these independently controlled negotiation capabilities.
func hlsApplicationTargetLimits(cfg config.TranscodingConfig, target identity.User) playback.ConversionLimits {
	limits := hlsServerLimits(cfg)
	limits.AllowRemux, limits.AllowAudioTranscode, limits.AllowVideoTranscode = false, false, false
	var policy map[string]json.RawMessage
	if !cfg.Enabled || !utf8.Valid(target.Policy) || json.Unmarshal(target.Policy, &policy) != nil || policy == nil {
		return limits
	}
	enabled := func(name string) bool {
		raw, exists := policy[name]
		if !exists {
			return true
		}
		var value *bool
		return json.Unmarshal(raw, &value) == nil && value != nil && *value
	}
	limits.AllowRemux = enabled("EnablePlaybackRemuxing")
	limits.AllowAudioTranscode = enabled("EnableAudioPlaybackTranscoding")
	limits.AllowVideoTranscode = enabled("EnableVideoPlaybackTranscoding")
	return limits
}

func hlsPlanAllowed(plan transcode.Plan, limits playback.ConversionLimits) bool {
	videoEncoded := plan.VideoCodec != "" && plan.VideoCodec != "copy"
	audioEncoded := plan.AudioCodec != "" && plan.AudioCodec != "copy"
	return (!videoEncoded || limits.AllowVideoTranscode) && (!audioEncoded || limits.AllowAudioTranscode) &&
		(videoEncoded || audioEncoded || limits.AllowRemux)
}

func (h *hlsRuntime) timeline(ctx context.Context, session *hlsSession, input *os.File) (transcode.Timeline, error) {
	for {
		session.mu.Lock()
		if session.closed {
			session.mu.Unlock()
			return transcode.Timeline{}, transcode.ErrJobNotFound
		}
		if session.timeline != nil {
			timeline := *session.timeline
			session.mu.Unlock()
			return timeline, nil
		}
		if pending := session.building; pending != nil {
			session.mu.Unlock()
			select {
			case <-pending:
				continue
			case <-ctx.Done():
				return transcode.Timeline{}, ctx.Err()
			case <-session.ctx.Done():
				return transcode.Timeline{}, transcode.ErrJobNotFound
			}
		}
		pending := make(chan struct{})
		session.building = pending
		session.mu.Unlock()
		workCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		stop := context.AfterFunc(session.ctx, cancel)
		var keys []int64
		var err error
		plan := session.key.plan
		copiedVideo := plan.VideoCodec == "copy"
		if copiedVideo {
			select {
			case h.probes <- struct{}{}:
				keys, err = transcode.Keyframes(workCtx, h.server.cfg.FFprobePath, input, plan.VideoStreamIndex, plan.DurationTicks)
				<-h.probes
			case <-workCtx.Done():
				err = workCtx.Err()
			}
		}
		var timeline transcode.Timeline
		if err == nil {
			if plan.VideoStreamIndex < 0 {
				if session.audioTiming == nil {
					err = transcode.ErrUnsupportedTimeline
				} else {
					options := transcode.AudioTimelineOptions{Codec: plan.AudioCodec, SampleRate: plan.AudioSampleRate}
					if plan.AudioCodec == "copy" {
						last := session.audioTiming.LastPacketStartTicks
						options.LastPacketStartTicks = &last
						options.MaxPacketDurationTicks = session.audioTiming.MaxPacketDurationTicks
					}
					timeline, err = transcode.BuildAudioTimeline(plan.DurationTicks, plan.SegmentSeconds, options)
				}
			} else {
				timeline, err = transcode.BuildTimeline(plan.DurationTicks, plan.SegmentSeconds, keys, copiedVideo)
			}
		}
		stop()
		cancel()
		session.mu.Lock()
		if session.closed {
			err = transcode.ErrJobNotFound
		}
		if err == nil {
			session.timeline = &timeline
			if copiedVideo {
				session.lead = keys[0]
			}
		}
		session.building = nil
		close(pending)
		session.mu.Unlock()
		return timeline, err
	}
}

// segment consumes input in every case. Nearby sequential requests wait for an
// existing producer; distant seeks start an immutable revision at the requested
// source boundary. Earlier published files may remain reusable until eviction.
func (h *hlsRuntime) segment(ctx context.Context, session *hlsSession, input *os.File, number int) (*transcode.ReadHandle, error) {
	defer input.Close()
	timeline, err := h.timeline(ctx, session, input)
	if err != nil {
		return nil, err
	}
	if number < 0 || number >= len(timeline.Segments) {
		return nil, transcode.ErrJobNotFound
	}
	name := "segment-" + paddedSegmentNumber(number) + ".ts"
	var gate *hlsAdmissionGate
	var retryFrom *hlsAdmission
	staleRetries := 0
	releaseReservation := func() {
		if gate != nil {
			reserved := gate
			gate = nil
			h.releaseAdmission(session.key, reserved)
		}
	}
	defer releaseReservation()
	for {
		session.mu.Lock()
		if session.closed || ctx.Err() != nil {
			session.mu.Unlock()
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return nil, transcode.ErrJobNotFound
		}
		if retryFrom != nil && session.admissionRevision != retryFrom.revision {
			session.mu.Unlock()
			return nil, context.Canceled
		}
		for index := len(session.producers) - 1; index >= 0; index-- {
			producer := session.producers[index]
			if h.producerReleased(producer) {
				continue
			}
			if number < producer.first || number > producer.last {
				continue
			}
			if handle, openErr := h.manager.TryOpen(session.key.scope, producer.id, name); openErr == nil {
				session.lastAsked, session.accessed = number, time.Now()
				session.mu.Unlock()
				releaseReservation()
				return handle, nil
			}
			state, stateErr := h.manager.Snapshot(session.key.scope, producer.id)
			if stateErr == nil && (state.State == "queued" || state.State == "running") && !session.demand.paused && h.producerProducing(producer) {
				session.lastAsked, session.accessed = number, time.Now()
				session.mu.Unlock()
				releaseReservation()
				return h.manager.Open(ctx, session.key.scope, producer.id, name)
			}
		}
		if session.demand.paused {
			session.mu.Unlock()
			return nil, transcode.ErrOutputUnavailable
		}
		if pending := session.admission; pending != nil && pending.ctx.Err() == nil {
			inside := number >= pending.first && number <= pending.last
			session.lastAsked, session.accessed = number, time.Now()
			session.mu.Unlock()
			releaseReservation()
			record, err := waitHLSAdmission(ctx, session, pending)
			if err != nil {
				if retryHLSAdmission(ctx, session, pending, err) {
					if errors.Is(err, errHLSAdmissionStale) {
						if staleRetries != 0 {
							return nil, transcode.ErrJobNotFound
						}
						staleRetries++
					}
					retryFrom = pending
					continue
				}
				return nil, err
			}
			if inside {
				return h.manager.Open(ctx, session.key.scope, record.ID, name)
			}
			// A GET has no demand generation. Waiting for an unrelated bounded
			// admission cannot revoke it or turn an old seek into current demand.
			continue
		}
		if gate == nil {
			session.mu.Unlock()
			gate, err = h.reserveAdmission(session.key)
			if err != nil {
				return nil, err
			}
			continue
		}
		plan := session.key.plan
		last := hlsProductionLast(timeline, number)
		cuts, err := timeline.BoundaryTicks(number, last)
		if err != nil {
			session.mu.Unlock()
			return nil, err
		}
		encodedCuts := make([]string, len(cuts))
		for index, cut := range cuts {
			encodedCuts[index] = strconv.FormatInt(cut, 10)
		}
		plan.SegmentMode, plan.SegmentStartNumber, plan.StartTicks = "vod", number, timeline.Segments[number].StartTicks
		plan.EndTicks = timeline.Segments[last].StartTicks + timeline.Segments[last].DurationTicks
		plan.SegmentTimes = strings.Join(encodedCuts, ",")
		if number == 0 {
			plan.ReferenceStartTicks = session.lead
		}
		if !h.makeProducerRoomLocked(session) {
			session.mu.Unlock()
			return nil, transcode.ErrBusy
		}
		producerInput, playbackInput, playbackWorker, err := h.duplicateAdmissionInputLocked(session, input)
		if err != nil {
			session.mu.Unlock()
			return nil, err
		}
		pending := newHLSAdmission(ctx, session, transcode.Spec{Scope: session.key.scope, SourceStamp: session.key.stamp, Plan: plan}, number, last)
		pending.installPlaybackInput(playbackInput, playbackWorker)
		session.admission, session.lastAsked, session.accessed = pending, number, time.Now()
		session.mu.Unlock()
		workerGate := gate
		gate = nil
		go h.runAdmission(session, pending, workerGate, producerInput)
		record, err := waitHLSAdmission(ctx, session, pending)
		if err != nil {
			if errors.Is(err, errHLSAdmissionStale) && retryHLSAdmission(ctx, session, pending, err) {
				if staleRetries != 0 {
					return nil, transcode.ErrJobNotFound
				}
				staleRetries++
				retryFrom = pending
				continue
			}
			return nil, err
		}
		return h.manager.Open(ctx, session.key.scope, record.ID, name)
	}
}

func paddedSegmentNumber(number int) string {
	value := strconv.Itoa(number)
	if len(value) < 6 {
		value = strings.Repeat("0", 6-len(value)) + value
	}
	return value
}

func (h *hlsRuntime) maintain() {
	defer h.workers.Done()
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-tick.C:
		}
		h.mu.Lock()
		sessions := make([]*hlsSession, 0, len(h.sessions))
		for _, session := range h.sessions {
			sessions = append(sessions, session)
		}
		h.mu.Unlock()
		cycle, cancel := context.WithTimeout(h.ctx, 4*time.Second)
		h.maintainSessions(cycle, sessions)
		cancel()
	}
}

func (h *hlsRuntime) maintainSessions(cycle context.Context, sessions []*hlsSession) {
	// Sweep idle registrations independently of the database budget. A slow
	// authorization check must not delay cancellation of an abandoned producer.
	type maintenanceCandidate struct {
		session *hlsSession
		checked time.Time
	}
	candidates := make([]maintenanceCandidate, 0, len(sessions))
	for _, session := range sessions {
		session.mu.Lock()
		h.releaseExpiredGeneratedWindowPinsLocked(session, time.Now())
		idle, active := time.Since(session.accessed) > hlsIdleTTL, !session.closed && len(session.producers) > 0
		checked := session.maintenanceChecked
		session.mu.Unlock()
		if idle {
			h.retire(session)
			continue
		}
		if active {
			candidates = append(candidates, maintenanceCandidate{session: session, checked: checked})
		}
	}
	// Check the least recently attempted sessions first, including checks that
	// exhausted the previous cycle. Bound database concurrency without letting
	// map iteration or one stalled credential repeatedly starve other clients.
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].checked.Before(candidates[j].checked) })
	var dispatch sync.Mutex
	next := 0
	var workers sync.WaitGroup
	for range min(hlsMaintenanceWorkers, len(candidates)) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for {
				dispatch.Lock()
				if cycle.Err() != nil || next == len(candidates) {
					dispatch.Unlock()
					return
				}
				session := candidates[next].session
				next++
				dispatch.Unlock()
				h.maintainSession(cycle, session)
			}
		}()
	}
	workers.Wait()
}

func (h *hlsRuntime) maintainSession(cycle context.Context, session *hlsSession) {
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		return
	}
	session.maintenanceChecked = time.Now()
	keepPresence := time.Since(session.presenceUpdated) >= time.Minute && time.Since(session.accessed) < time.Minute
	session.mu.Unlock()
	check, stop := context.WithTimeout(cycle, 750*time.Millisecond)
	var file *os.File
	var loan *hlsPlaybackSource
	var err error
	if h.usesPlaybackOwnership(session.key.plan) {
		loan, _, err = h.authorizePlaybackSource(check, session.principal, session)
		if loan != nil {
			file, check = loan.file, loan.context(check)
		}
	} else {
		file, _, err = h.verify(check, session.principal, session.key.scope, session.key.stamp, session.key.plan)
	}
	if err == nil && keepPresence && h.server != nil {
		// Media activity keeps the prepared play alive without pretending
		// that the client reported a position, play count or watched state.
		play, _, pingErr := h.server.library.ReportPlayback(check, playbackOwner(session.principal), library.PlaybackReport{
			Event: "Ping", PlaySessionID: session.key.scope.PlaySessionID,
			ItemID: session.key.scope.ItemID, MediaSourceID: session.key.scope.SourceID})
		if pingErr != nil {
			err = pingErr
		} else if play.State == "Stopped" || play.State == "Expired" {
			err = library.ErrNotFound
		} else {
			session.mu.Lock()
			session.presenceUpdated = time.Now()
			session.mu.Unlock()
		}
	}
	stop()
	if loan != nil {
		err = errors.Join(err, loan.close())
	} else if file != nil {
		_ = file.Close()
	}
	if permanentHLSError(err) {
		h.retire(session)
	}
}

func (h *hlsRuntime) Close(ctx context.Context) error {
	if h == nil {
		return nil
	}
	h.once.Do(func() {
		h.mu.Lock()
		h.closing = true
		h.cancel()
		h.mu.Unlock()
		go func() {
			h.mu.Lock()
			sessions := make([]*hlsSession, 0, len(h.sessions))
			for _, session := range h.sessions {
				sessions = append(sessions, session)
			}
			h.mu.Unlock()
			for _, session := range sessions {
				h.retire(session)
			}
			h.initializationBudget.close()
			h.closeErr = h.manager.Close(context.Background())
			h.requests.Wait()
			h.workers.Wait()
			close(h.done)
		}()
	})
	select {
	case <-h.done:
		return h.closeErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
