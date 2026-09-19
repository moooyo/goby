package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moooyo/goby/internal/dynamicsource"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/timeshift"
	"github.com/moooyo/goby/internal/transcode"
)

type dynamicStreamKey struct {
	owner  dynamicsource.Owner
	liveID string
	plan   transcode.Plan
}

type dynamicStreamSession struct {
	mu              sync.Mutex
	id              string
	key             dynamicStreamKey
	scope           transcode.Scope
	request         playback.Request
	output          playback.Source
	subtitleSource  playback.Source
	subtitleView    playback.HLSSubtitleView
	principal       identity.Principal
	windowID        string
	generation      uint64
	producerStarted bool
	producerEnded   bool
	changed         chan struct{}
	subtitles       *dynamicSubtitleRuntime
	clockGeneration uint64
	clockDeltaTicks int64
	lastSequence    int64
	jobID           string
	input           *dynamicsource.Input
	infinite        bool
	accessed        time.Time
	presenceUpdated time.Time
	ctx             context.Context
	cancel          context.CancelFunc
	closed          bool
}

type dynamicStreamRuntime struct {
	mu         sync.Mutex
	sessions   map[string]*dynamicStreamSession
	byKey      map[dynamicStreamKey]*dynamicStreamSession
	closing    bool
	store      *timeshift.Store
	workers    sync.WaitGroup
	revalidate func(context.Context, identity.Principal) (identity.Principal, error)
}

func (s *Server) initializeDynamicSources(_ context.Context) error {
	definitions := make([]dynamicsource.Definition, 0, len(s.cfg.DynamicSources))
	for _, configured := range s.cfg.DynamicSources {
		definitions = append(definitions, dynamicsource.Definition{ItemID: configured.ItemID, Name: configured.Name,
			URL: configured.URL, Headers: configured.Headers, Infinite: configured.Infinite, MaxReconnects: configured.MaxReconnects, Subtitles: configured.Subtitles})
	}
	manager, err := dynamicsource.New(context.Background(), definitions, dynamicsource.Options{
		Prober: media.Prober{FFprobePath: s.cfg.FFprobePath, Timeout: 10 * time.Second},
		Authorize: func(ctx context.Context, owner dynamicsource.Owner, itemID, playID string) error {
			subject := library.Subject{UserID: owner.UserID}
			if owner.ApplicationKey {
				subject.ApplicationCredentialID = owner.SessionID
			}
			item, err := s.library.GetItemFor(ctx, subject, itemID)
			if err != nil {
				return err
			}
			if !item.CanPlay || item.IsFolder {
				return library.ErrForbidden
			}
			if playID != "" {
				play, err := s.library.GetPlaybackSession(ctx, library.PlaybackOwner{UserID: owner.UserID, SessionID: owner.SessionID,
					DeviceID: owner.DeviceID, PeerIP: owner.PeerIP, ApplicationKey: owner.ApplicationKey, ApplicationClientID: owner.ApplicationClientID}, playID)
				if err != nil {
					return err
				}
				if play.ItemID != itemID || play.MediaSourceID != media.SourceID(itemID) || !time.Now().Before(play.ExpiresAt) ||
					(play.State != "Prepared" && play.State != "Playing" && play.State != "Paused") {
					return library.ErrNotFound
				}
			}
			return nil
		},
	})
	if err != nil {
		return err
	}
	s.dynamicSources = manager
	s.dynamicStreams = &dynamicStreamRuntime{sessions: make(map[string]*dynamicStreamSession), byKey: make(map[dynamicStreamKey]*dynamicStreamSession), revalidate: s.identity.RevalidateSession}
	if len(definitions) > 0 && s.cfg.Transcoding.Enabled && s.cfg.Timeshift.Enabled {
		store, err := timeshift.New(s.cfg.Timeshift.Options())
		if err != nil {
			return err
		}
		s.dynamicStreams.store = store
	}
	return nil
}

func (s *Server) dynamicLimits(ctx context.Context, principal identity.Principal, request playback.Request, r *http.Request) (playback.ConversionLimits, error) {
	limits := hlsPrincipalLimits(s.requestPlanningConfig(r), principal)
	if principal.IsApplicationKey() && request.DeviceProfile != nil {
		if request.UserID == "" {
			return limits, playback.ErrInvalidRequest
		}
		target, err := s.identity.GetUser(ctx, request.UserID)
		if err != nil {
			return limits, err
		}
		limits = hlsApplicationTargetLimits(s.requestPlanningConfig(r), target)
	}
	return applyPrincipalRemoteBitrateLimit(limits, principal), nil
}

func dynamicPlaybackSource(lease dynamicsource.Lease) playback.Source {
	return playback.Source{ItemID: lease.ItemID, MediaSourceID: lease.SourceID, ItemType: lease.ItemType, Info: lease.Info}
}

func (s *Server) freshDynamicPrincipal(ctx context.Context, principal identity.Principal) (identity.Principal, error) {
	if s.dynamicStreams == nil || s.dynamicStreams.revalidate == nil {
		return identity.Principal{}, identity.ErrUnauthorized
	}
	fresh, err := s.dynamicStreams.revalidate(ctx, principal)
	if err != nil {
		return identity.Principal{}, err
	}
	if dynamicSourceOwner(fresh) != dynamicSourceOwner(principal) {
		return identity.Principal{}, identity.ErrUnauthorized
	}
	return fresh, nil
}

func (s *Server) dynamicPlaybackDTO(r *http.Request, principal identity.Principal, lease dynamicsource.Lease, request playback.Request) (map[string]any, error) {
	if s.dynamicStreams == nil {
		return nil, dynamicsource.ErrUnavailable
	}
	principal, err := s.freshDynamicPrincipal(r.Context(), principal)
	if err != nil {
		return nil, err
	}
	limits, err := s.dynamicLimits(r.Context(), principal, request, r)
	if err != nil {
		return nil, err
	}
	start := request.StartTimeTicks
	if start != nil && *start < 0 {
		return nil, playback.ErrInvalidRequest
	}
	// Standard opening requests use zero for a fresh live start. An explicit
	// retained-position query on a published HLS URL remains a real seek.
	if start != nil && *start == 0 {
		start = nil
	}
	request.StartTimeTicks = nil
	conversion, err := playback.PlanDynamicConversion(dynamicPlaybackSource(lease), request, limits)
	if err != nil {
		return nil, err
	}
	if request.DeviceProfile == nil {
		return dynamicSourceDTO(lease), nil
	}
	if s.hls == nil || !s.hls.health().Available || s.dynamicStreams.store == nil {
		return nil, dynamicsource.ErrUnavailable
	}
	if conversion.Plan == nil {
		return nil, errHLSRequestUnsupported
	}
	if !principalPlanBitrateAllowed(principal, library.MediaFile{Item: library.Item{Media: &lease.Info}}, *conversion.Plan) {
		// Copy admission conservatively reserves the full known source rate.
		// A fully encoded output can still fit the current remote budget.
		disabled := false
		request.AllowVideoStreamCopy, request.AllowAudioStreamCopy = &disabled, &disabled
		conversion, err = playback.PlanDynamicConversion(dynamicPlaybackSource(lease), request, limits)
		if err != nil {
			return nil, err
		}
		if conversion.Plan == nil || !principalPlanBitrateAllowed(principal, library.MediaFile{Item: library.Item{Media: &lease.Info}}, *conversion.Plan) {
			return nil, library.ErrForbidden
		}
	}
	conversion, err = s.resolveHardwareEncoding(r.Context(), limits, conversion, r.Method != http.MethodHead)
	if err != nil {
		return nil, err
	}
	keyPlan := *conversion.Plan
	// The source origin belongs to a connection generation. Reconnection may
	// change it while the authorized codec, processing and track set stay fixed.
	keyPlan.SourceFormatStartKnown, keyPlan.SourceFormatStartTicks = false, 0
	key := dynamicStreamKey{owner: dynamicSourceOwner(principal).Identity(), liveID: lease.ID, plan: keyPlan}
	runtime := s.dynamicStreams
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.closing {
		return nil, dynamicsource.ErrClosed
	}
	for id, previous := range runtime.sessions {
		previous.mu.Lock()
		expired := previous.closed || time.Since(previous.accessed) >= hlsIdleTTL
		replaced := !expired && previous.key.owner == key.owner && previous.key.liveID == key.liveID && previous.key.plan != key.plan
		previous.mu.Unlock()
		if replaced {
			s.stopDynamicProducer(previous)
			if runtime.byKey[previous.key] == previous {
				delete(runtime.byKey, previous.key)
			}
		}
		if expired {
			s.retireDynamicSession(previous)
			delete(runtime.sessions, id)
			if runtime.byKey[previous.key] == previous {
				delete(runtime.byKey, previous.key)
			}
		}
	}
	session := runtime.byKey[key]
	if session == nil {
		count := 0
		for _, previous := range runtime.sessions {
			if previous.key.owner == key.owner {
				count++
			}
		}
		if len(runtime.sessions) >= maxHLSSessions || count >= maxHLSAuthSessions {
			return nil, dynamicsource.ErrBusy
		}
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, dynamicsource.ErrUnavailable
		}
		lifetime, cancel := context.WithCancel(s.hls.ctx)
		session = &dynamicStreamSession{id: hex.EncodeToString(random[:]), key: key, request: request, output: conversion.OutputSource, infinite: lease.Infinite,
			subtitleSource: dynamicPlaybackSource(lease), subtitleView: conversion.SubtitleView, principal: principal, changed: make(chan struct{}),
			accessed: time.Now(), presenceUpdated: time.Now(), ctx: lifetime, cancel: cancel, scope: transcode.Scope{UserID: principal.User.ID, AuthSessionID: principal.SessionID,
				DeviceID: principal.Client.DeviceID, ApplicationKey: principal.IsApplicationKey(), ApplicationClientID: principal.ClientSessionID,
				PlaySessionID: lease.PlaySessionID, ItemID: lease.ItemID, SourceID: lease.SourceID}}
		windowOptions := s.cfg.Timeshift.WindowOptions(dynamicVariants(key.plan))
		windowOptions.TargetDurationTicks = dynamicTargetDuration(key.plan)
		window, err := runtime.store.Create(r.Context(), timeshiftScope(session.scope), windowOptions)
		if err != nil {
			cancel()
			return nil, err
		}
		session.windowID = window.PresentationID
		runtime.sessions[session.id], runtime.byKey[key] = session, session
		runtime.workers.Add(1)
		go s.maintainDynamicPresentation(session)
	}
	token, _, _ := parseEmbyCredentials(r)
	dto := dynamicSourceDTO(lease)
	view := dynamicPlaybackView{Subtitles: conversion.SubtitleView, StartTicks: start}
	dto["TranscodingContainer"], dto["TranscodingSubProtocol"] = conversion.Plan.Container, "hls"
	if conversion.Method == "DirectStream" && request.EnableTranscoding != nil && !*request.EnableTranscoding {
		dto["SupportsDirectStream"], dto["DirectStreamUrl"] = true, dynamicArtifactURLView(session, "master.m3u8", token, view, true)
	} else {
		dto["SupportsTranscoding"], dto["TranscodingUrl"] = true, dynamicArtifactURLView(session, "master.m3u8", token, view, true)
	}
	if conversion.Output.DefaultAudioStreamIndex != nil {
		dto["DefaultAudioStreamIndex"] = *conversion.Output.DefaultAudioStreamIndex
	}
	dto["DefaultSubtitleStreamIndex"] = -1
	if conversion.SubtitleView.SelectionSet {
		dto["DefaultSubtitleStreamIndex"] = conversion.SubtitleView.SelectedStreamIndex
	}
	if conversion.Output.DefaultSubtitleStreamIndex != nil {
		dto["DefaultSubtitleStreamIndex"] = *conversion.Output.DefaultSubtitleStreamIndex
	}
	dto["SupportsSeeking"], dto["SupportsPause"] = true, true
	dto["GobyWindowUrl"] = dynamicArtifactURLView(session, "window.json", token, view, false)
	return dto, nil
}

// tryDynamicPlaybackInfo intercepts only operator-registered catalog sources.
func (s *Server) tryDynamicPlaybackInfo(w http.ResponseWriter, r *http.Request, request playback.Request) bool {
	if s.dynamicSources == nil {
		return false
	}
	principal := r.Context().Value(principalKey).(identity.Principal)
	owner := dynamicSourceOwner(principal)
	description, err := s.dynamicSources.Describe(r.Context(), owner, request.ID, request.MediaSourceID)
	if errors.Is(err, dynamicsource.ErrNotFound) {
		if request.LiveStreamID == "" {
			return false
		}
		s.liveStreamError(w, r, err)
		return true
	}
	if err != nil {
		s.liveStreamError(w, r, err)
		return true
	}
	play, err := s.library.PrepareDynamicPlayback(r.Context(), playbackOwner(principal), description.ItemID, description.SourceID, request.CurrentPlaySessionID)
	if err != nil {
		s.playbackError(w, r, err)
		return true
	}
	request.ID, request.MediaSourceID, request.CurrentPlaySessionID = description.ItemID, description.SourceID, play.ID
	if request.LiveStreamID == "" && (request.AutoOpenLiveStream == nil || !*request.AutoOpenLiveStream) {
		jsonResponse(w, http.StatusOK, map[string]any{"MediaSources": []map[string]any{describeDynamicSourceDTO(description)}, "PlaySessionId": play.ID})
		return true
	}
	var lease dynamicsource.Lease
	if request.LiveStreamID != "" {
		lease, err = s.dynamicSources.Info(r.Context(), owner, request.LiveStreamID)
		if err == nil && (lease.ItemID != description.ItemID || lease.PlaySessionID != play.ID) {
			err = dynamicsource.ErrNotFound
		}
	} else {
		lease, err = s.dynamicSources.Open(r.Context(), owner, dynamicsource.OpenRequest{ItemID: description.ItemID, OpenToken: description.OpenToken, PlaySessionID: play.ID})
	}
	if err != nil {
		s.liveStreamError(w, r, err)
		return true
	}
	ApplyDynamicUserPreferences(principal, lease.Info, &request)
	dto, err := s.dynamicPlaybackDTO(r, principal, lease, request)
	if err != nil {
		if request.LiveStreamID == "" {
			_ = s.dynamicSources.CloseLease(context.Background(), owner, lease.ID)
		}
		s.liveStreamError(w, r, err)
		return true
	}
	jsonResponse(w, http.StatusOK, map[string]any{"MediaSources": []map[string]any{dto}, "PlaySessionId": play.ID})
	return true
}

func dynamicArtifactURL(session *dynamicStreamSession, name, token string) string {
	return dynamicArtifactURLView(session, name, token, dynamicPlaybackView{Subtitles: session.subtitleView}, strings.HasSuffix(name, ".m3u8"))
}

func (s *Server) dynamicMediaInfoDTO(r *http.Request, principal identity.Principal, lease dynamicsource.Lease) (map[string]any, error) {
	if s.dynamicStreams == nil {
		return dynamicSourceDTO(lease), nil
	}
	s.dynamicStreams.mu.Lock()
	var selected *dynamicStreamSession
	for _, session := range s.dynamicStreams.sessions {
		if session.key.owner == dynamicSourceOwner(principal).Identity() && session.key.liveID == lease.ID && s.dynamicStreams.byKey[session.key] == session {
			selected = session
			break
		}
	}
	s.dynamicStreams.mu.Unlock()
	if selected == nil {
		return dynamicSourceDTO(lease), nil
	}
	return s.dynamicPlaybackDTO(r, principal, lease, selected.request)
}

func (s *Server) findDynamicSession(r *http.Request, values map[string]string) (*dynamicStreamSession, dynamicsource.Lease, error) {
	if s.dynamicStreams == nil {
		return nil, dynamicsource.Lease{}, dynamicsource.ErrNotFound
	}
	for name := range values {
		switch name {
		case "gobyliveid", "playsessionid", "mediasourceid", "deviceid", "api_key", "starttimeticks", "subtitlestreamindex", "subtitleoffsetticks", "live":
		default:
			return nil, dynamicsource.Lease{}, dynamicsource.ErrInvalid
		}
	}
	principal, err := s.freshDynamicPrincipal(r.Context(), r.Context().Value(principalKey).(identity.Principal))
	if err != nil {
		return nil, dynamicsource.Lease{}, err
	}
	runtime := s.dynamicStreams
	runtime.mu.Lock()
	session := runtime.sessions[values["gobyliveid"]]
	closed := runtime.closing
	runtime.mu.Unlock()
	if closed || session == nil || session.key.owner != dynamicSourceOwner(principal).Identity() || session.key.liveID != r.PathValue("LiveStreamId") {
		return nil, dynamicsource.Lease{}, dynamicsource.ErrNotFound
	}
	for name, expected := range map[string]string{"playsessionid": session.scope.PlaySessionID, "mediasourceid": session.scope.SourceID, "deviceid": session.scope.DeviceID} {
		if value, exists := values[name]; exists && value != expected {
			return nil, dynamicsource.Lease{}, dynamicsource.ErrNotFound
		}
	}
	if _, err := dynamicRequestView(values, session); err != nil {
		return nil, dynamicsource.Lease{}, dynamicsource.ErrInvalid
	}
	if err := s.checkMediaPolicy(principal, session.scope); err != nil {
		s.retireDynamicSession(session)
		return nil, dynamicsource.Lease{}, err
	}
	lease, err := s.dynamicSources.Info(r.Context(), dynamicSourceOwner(principal), session.key.liveID)
	if err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, dynamicsource.ErrBusy) {
			s.retireDynamicSession(session)
		}
		return nil, lease, err
	}
	limits, err := s.dynamicLimits(r.Context(), principal, session.request, r)
	if err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			s.retireDynamicSession(session)
		}
		return nil, lease, err
	}
	session.mu.Lock()
	planningSource := dynamicPlaybackSource(lease)
	if session.producerEnded {
		planningSource = session.subtitleSource
	}
	session.mu.Unlock()
	conversion, err := playback.PlanDynamicConversion(planningSource, session.request, limits)
	actualPlan, matches := dynamicEncodingRevision(conversion.Plan, session.key.plan)
	if err != nil || !matches || !principalPlanBitrateAllowed(principal, library.MediaFile{Item: library.Item{Media: &lease.Info}}, session.key.plan) {
		s.retireDynamicSession(session)
		return nil, lease, library.ErrForbidden
	}
	if *conversion.Plan != actualPlan {
		conversion, err = playback.ReprojectVideoEncodingOutput(conversion, actualPlan)
		if err != nil || conversion.Plan == nil {
			s.retireDynamicSession(session)
			return nil, lease, library.ErrForbidden
		}
	}
	session.mu.Lock()
	if session.closed {
		session.mu.Unlock()
		return nil, lease, dynamicsource.ErrNotFound
	}
	session.accessed = time.Now()
	refreshPresence := r.Method != http.MethodHead && s.library != nil && time.Since(session.presenceUpdated) >= 20*time.Second
	if refreshPresence {
		session.presenceUpdated = time.Now()
	}
	session.mu.Unlock()
	if r.Method != http.MethodHead {
		s.touchDynamicProducer(session)
	}
	if refreshPresence {
		_, _, err := s.library.ReportPlayback(r.Context(), playbackOwner(principal), library.PlaybackReport{Event: "Ping", PlaySessionID: session.scope.PlaySessionID, ItemID: session.scope.ItemID, MediaSourceID: session.scope.SourceID})
		if err != nil {
			return nil, lease, err
		}
	}
	return session, lease, nil
}

func (s *Server) ensureDynamicJob(ctx context.Context, r *http.Request, session *dynamicStreamSession) (string, error) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return "", dynamicsource.ErrNotFound
	}
	if session.producerStarted {
		return session.jobID, nil
	}
	if r.Method == http.MethodHead {
		return "", dynamicsource.ErrNotFound
	}
	if err := s.startDynamicEpochLocked(ctx, session); err != nil {
		return "", err
	}
	session.producerStarted = true
	signalDynamicSessionLocked(session)
	return session.jobID, nil
}

type dynamicStreamJobs interface {
	EnsureStreamInputs(context.Context, transcode.Spec, transcode.StreamInputs) (transcode.Record, error)
}

// The caller serializes generation changes with session.mu. A new upstream
// connection always receives a new encoder job and a new retained media epoch.
func (s *Server) startDynamicEpochLocked(ctx context.Context, session *dynamicStreamSession) error {
	engine, ok := s.hls.manager.(dynamicStreamJobs)
	if !ok {
		return dynamicsource.ErrUnavailable
	}
	principal, err := s.freshDynamicPrincipal(ctx, session.principal)
	if err != nil {
		return err
	}
	input, err := s.dynamicSources.Acquire(ctx, dynamicSourceOwner(principal), session.key.liveID)
	if err != nil {
		return err
	}
	closeInput := true
	defer func() {
		if closeInput {
			_ = input.Close()
			if session.subtitles != nil {
				session.subtitles.CancelEpoch(input.Generation)
			}
		}
	}()
	request := &http.Request{Method: http.MethodGet}
	request = request.WithContext(context.WithValue(ctx, principalKey, principal))
	limits, err := s.dynamicLimits(ctx, principal, session.request, request)
	if err != nil {
		return err
	}
	conversion, err := playback.PlanDynamicConversion(dynamicPlaybackSource(input.Lease), session.request, limits)
	actualPlan, matches := dynamicEncodingRevision(conversion.Plan, session.key.plan)
	if err != nil || !matches ||
		!principalPlanBitrateAllowed(principal, library.MediaFile{Item: library.Item{Media: &input.Info}}, session.key.plan) {
		return errHLSRequestUnsupported
	}
	conversion, err = playback.ReprojectVideoEncodingOutput(conversion, actualPlan)
	if err != nil || conversion.Plan == nil {
		return errHLSRequestUnsupported
	}
	count := 1
	if session.key.plan.Subtitle.Mode == "burn" {
		count = 2
	}
	pipes, err := input.OpenPipeSet(count)
	if err != nil {
		return err
	}
	inputs := transcode.StreamInputs{Media: pipes.Readers[0]}
	if count == 2 {
		inputs.Bitmap = pipes.Readers[1]
	}
	session.generation = input.Generation
	if err := s.beginDynamicSubtitlesLocked(session, input.Lease, actualPlan); err != nil {
		_ = pipes.Close()
		return err
	}
	record, err := engine.EnsureStreamInputs(ctx, transcode.Spec{Scope: session.scope, SourceStamp: input.Stamp, Plan: actualPlan}, inputs)
	if err != nil {
		_ = pipes.Close()
		return err
	}
	session.input, session.jobID, session.output, session.principal = input, record.ID, conversion.OutputSource, principal
	session.subtitleSource = dynamicPlaybackSource(input.Lease)
	closeInput = false
	return nil
}

func signalDynamicSessionLocked(session *dynamicStreamSession) {
	if session.changed != nil {
		close(session.changed)
	}
	session.changed = make(chan struct{})
}

func (s *Server) maintainDynamicPresentation(session *dynamicStreamSession) {
	defer s.dynamicStreams.workers.Done()
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	for {
		select {
		case <-session.ctx.Done():
			return
		case <-timer.C:
		}
		session.mu.Lock()
		if session.closed {
			session.mu.Unlock()
			return
		}
		if time.Since(session.accessed) >= hlsIdleTTL {
			session.mu.Unlock()
			s.retireDynamicSession(session)
			return
		}
		if !session.producerStarted || session.producerEnded {
			session.mu.Unlock()
			continue
		}
		record, err := s.hls.manager.Snapshot(session.scope, session.jobID)
		if err == nil && (record.State == "queued" || record.State == "running") {
			session.mu.Unlock()
			continue
		}
		// Published epochs already belong to the timeshift store. Retire this
		// terminal job's scratch before replacing its only session reference;
		// otherwise a later stop can cancel only the newest job and old caches
		// remain until their unrelated idle TTL expires.
		if session.jobID != "" {
			cancelErr := s.hls.manager.CancelJob(session.jobID, session.scope)
			if cancelErr != nil && !errors.Is(cancelErr, transcode.ErrJobNotFound) {
				session.mu.Unlock()
				continue
			}
		}
		if session.input != nil {
			_ = session.input.Close()
			session.input = nil
		}
		if session.infinite && session.ctx.Err() == nil {
			_ = s.dynamicStreams.store.SetState(session.ctx, timeshiftScope(session.scope), session.windowID, timeshift.State{Stalled: true})
			opening, cancel := context.WithTimeout(session.ctx, 30*time.Second)
			err = s.startDynamicEpochLocked(opening, session)
			cancel()
			if err == nil {
				signalDynamicSessionLocked(session)
				session.mu.Unlock()
				continue
			}
		}
		session.producerEnded = true
		state := timeshift.State{Ended: !session.infinite && err == nil && record.State == "completed", Stalled: session.infinite || err != nil || record.State != "completed"}
		_ = s.dynamicStreams.store.SetState(session.ctx, timeshiftScope(session.scope), session.windowID, state)
		signalDynamicSessionLocked(session)
		session.mu.Unlock()
	}
}
func dynamicMasterPlaylist(session *dynamicStreamSession, token string) ([]byte, error) {
	return dynamicMasterPlaylistView(session, token, dynamicPlaybackView{Subtitles: session.subtitleView})
}

func dynamicMasterPlaylistView(session *dynamicStreamSession, token string, view dynamicPlaybackView) ([]byte, error) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return nil, dynamicsource.ErrNotFound
	}
	plan := session.key.plan
	var result strings.Builder
	result.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n")
	tracks := transcode.PlanHLSSubtitles(plan)
	for slot := 0; slot < tracks.Count; slot++ {
		metadata, ok := playback.HLSSubtitleMetadata(session.subtitleSource, plan, slot)
		if !ok {
			return nil, errInvalidHLSManifest
		}
		label := hlsSubtitleLabel(metadata.Title)
		if label == "" {
			label = hlsSubtitleLabel(metadata.Language)
		}
		if label == "" {
			label = "Subtitle"
		}
		label += " [" + strconv.Itoa(metadata.Index) + "]"
		selected := "NO"
		if view.Subtitles.SelectedStreamIndex == metadata.Index {
			selected = "YES"
		}
		address := dynamicArtifactURLView(session, hlsSubtitlePlaylistName(slot), token, view, true)
		if !validHLSManifestURL(address) {
			return nil, errInvalidHLSManifest
		}
		fmt.Fprintf(&result, "#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID=\"subs\",NAME=\"%s\",DEFAULT=%s,AUTOSELECT=%s", label, selected, selected)
		if language := hlsSubtitleLabel(metadata.Language); language != "" {
			fmt.Fprintf(&result, ",LANGUAGE=\"%s\"", language)
		}
		fmt.Fprintf(&result, ",URI=\"%s\"\n", address)
	}
	for index := 0; index < max(1, plan.HLS.RenditionCount); index++ {
		bandwidth, width, height := session.output.Info.Bitrate, 0, 0
		for _, stream := range session.output.Info.Streams {
			if stream.CodecType == "video" {
				width, height = stream.Width, stream.Height
				break
			}
		}
		if plan.HLS.RenditionCount > 0 {
			rendition := plan.HLS.Renditions[index]
			width, height = rendition.Width, rendition.Height
			audio := plan.AudioBitrate
			for _, stream := range session.output.Info.Streams {
				if stream.CodecType == "audio" {
					audio = max(audio, stream.Bitrate, session.output.Info.Bitrate*9/10-plan.VideoBitrate)
				}
			}
			bandwidth = (rendition.VideoBitrate + audio) * 10 / 9
		}
		child := dynamicArtifactURLView(session, transcode.HLSPlaylistName(index, plan.HLS.RenditionCount), token, view, true)
		if bandwidth <= 0 || !validHLSManifestURL(child) {
			return nil, errInvalidHLSManifest
		}
		fmt.Fprintf(&result, "#EXT-X-STREAM-INF:BANDWIDTH=%d", bandwidth)
		if width > 0 && height > 0 {
			fmt.Fprintf(&result, ",RESOLUTION=%dx%d", width, height)
		}
		if tracks.Count > 0 {
			result.WriteString(",SUBTITLES=\"subs\"")
		}
		fmt.Fprintf(&result, "\n%s\n", child)
	}
	return []byte(result.String()), nil
}

func (s *Server) dynamicHLSArtifact(w http.ResponseWriter, r *http.Request) {
	ctx, done, ok := s.beginHLS(w, r)
	if !ok {
		return
	}
	defer done()
	values, err := hlsValues(r)
	if err != nil {
		s.liveStreamError(w, r, dynamicsource.ErrInvalid)
		return
	}
	principal := r.Context().Value(principalKey).(identity.Principal)
	principal, err = s.bindKeyPlaybackContext(r, principal, values["playsessionid"])
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	r = r.WithContext(context.WithValue(ctx, principalKey, principal))
	session, _, err := s.findDynamicSession(r, values)
	if err != nil {
		s.dynamicWindowError(w, r, err)
		return
	}
	view, err := dynamicRequestView(values, session)
	if err != nil {
		s.dynamicWindowError(w, r, err)
		return
	}
	work, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(session.ctx, cancel)
	defer func() { stop(); cancel() }()
	token, _, _ := parseEmbyCredentials(r)
	name := r.PathValue("Artifact")
	if name == "master.m3u8" {
		body, err := dynamicMasterPlaylistView(session, token, view)
		if err != nil {
			s.dynamicWindowError(w, r, err)
			return
		}
		if _, _, err = s.findDynamicSession(r, values); err != nil {
			s.dynamicWindowError(w, r, err)
			return
		}
		writeDynamicManifest(w, r, body)
		return
	}
	if s.dynamicStreams.store == nil {
		s.liveStreamError(w, r, dynamicsource.ErrUnavailable)
		return
	}
	artifactID, opaqueArtifact := dynamicOpaqueArtifactID(name)
	_, mediaPlaylist := dynamicMediaPlaylistIndex(session.key.plan, name)
	_, subtitleSequence, _, subtitleResource := hlsSubtitleArtifact(session.key.plan, name, view.Subtitles)
	if !opaqueArtifact && !mediaPlaylist && !subtitleResource && name != "window.json" {
		s.dynamicWindowError(w, r, timeshift.ErrNotFound)
		return
	}
	if opaqueArtifact {
		artifact, variant, err := s.dynamicStreams.store.ResolveArtifact(work, timeshiftScope(session.scope), session.windowID, artifactID)
		if err != nil {
			s.dynamicWindowError(w, r, err)
			return
		}
		if name != dynamicArtifactName(artifact.ID, variant.Format, artifact.Initialization) {
			s.dynamicWindowError(w, r, timeshift.ErrNotFound)
			return
		}
	}
	policyAdmitted, delivered, mediaDelivered := false, false, false
	var releasePolicy func()
	defer func() {
		// Record delivery before release cancels the policy context. A running
		// producer keeps its bounded idle lease across retryable startup waits.
		session.mu.Lock()
		started := session.producerStarted
		session.mu.Unlock()
		if policyAdmitted && !delivered && !started {
			s.failMediaPolicy(work, session.scope)
		} else if policyAdmitted && mediaDelivered && work.Err() == nil {
			s.touchMediaPolicy(work, principal, session.scope)
		}
		if releasePolicy != nil {
			releasePolicy()
		}
	}()
	if r.Method != http.MethodHead {
		fresh, err := s.freshDynamicPrincipal(work, principal)
		if err != nil {
			s.identityError(w, r, err)
			return
		}
		policyContext, release, err := s.acquireMediaPolicy(work, fresh, session.scope)
		if err != nil {
			s.hlsError(w, r, err)
			return
		}
		releasePolicy = release
		policyAdmitted, principal, work = true, fresh, policyContext
		r = r.WithContext(context.WithValue(work, principalKey, fresh))
	}
	if opaqueArtifact {
		delivered = s.serveDynamicMediaArtifact(w, r, session, name, artifactID)
		mediaDelivered = delivered
		return
	}
	if _, err := s.ensureDynamicJob(work, r, session); err != nil {
		s.dynamicWindowError(w, r, err)
		return
	}
	if view.StartTicks != nil {
		current, err := s.dynamicStreams.store.Snapshot(work, timeshiftScope(session.scope), session.windowID)
		if err != nil {
			s.dynamicWindowError(w, r, err)
			return
		}
		if *view.StartTicks < current.EarliestTicks {
			s.dynamicWindowError(w, r, timeshift.ErrWindowExpired)
			return
		}
	}
	var snapshot timeshift.WindowSnapshot
	if subtitleResource && subtitleSequence >= 0 {
		snapshot, err = s.dynamicStreams.store.Snapshot(work, timeshiftScope(session.scope), session.windowID)
	} else {
		snapshot, err = s.dynamicMediaWindow(work, session, r.Method != http.MethodHead, name != "window.json")
	}
	if err != nil {
		if view.StartTicks != nil {
			current, inspectErr := s.dynamicStreams.store.Snapshot(work, timeshiftScope(session.scope), session.windowID)
			if inspectErr == nil && *view.StartTicks < current.EarliestTicks {
				err = timeshift.ErrWindowExpired
			}
		}
		s.dynamicWindowError(w, r, err)
		return
	}
	if _, _, err := s.findDynamicSession(r, values); err != nil {
		s.dynamicWindowError(w, r, err)
		return
	}
	if name == "window.json" {
		delivered = true
		writeDynamicWindow(w, r, session, snapshot, token, view)
		return
	}
	if index, ok := dynamicMediaPlaylistIndex(session.key.plan, name); ok {
		var body []byte
		for attempt := 0; attempt < 3; attempt++ {
			body, err = dynamicWindowPlaylist(snapshot, "r"+strconv.Itoa(index), view, func(id, format string, init bool) string {
				return dynamicArtifactURLView(session, dynamicArtifactName(id, format, init), token, view, false)
			})
			if err != nil {
				break
			}
			err = s.dynamicStreams.store.Advertise(work, timeshiftScope(session.scope), session.windowID, snapshot.Revision)
			if !errors.Is(err, timeshift.ErrSnapshotChanged) {
				break
			}
			snapshot, err = s.dynamicStreams.store.Snapshot(work, timeshiftScope(session.scope), session.windowID)
			if err != nil {
				break
			}
			err = timeshift.ErrSnapshotChanged
		}
		if err != nil {
			s.dynamicWindowError(w, r, err)
			return
		}
		delivered = true
		writeDynamicManifest(w, r, body)
		return
	}
	if slot, sequence, playlist, valid := hlsSubtitleArtifact(session.key.plan, name, view.Subtitles); valid {
		delivered = s.serveDynamicSubtitle(w, r, session, snapshot, slot, sequence, playlist, token, view)
		return
	}
	s.dynamicWindowError(w, r, timeshift.ErrNotFound)
}

func writeDynamicManifest(w http.ResponseWriter, r *http.Request, body []byte) {
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		_, _ = w.Write(body)
	}
}
func (s *Server) retireDynamicSession(session *dynamicStreamSession) {
	session.cancel()
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return
	}
	session.closed = true
	signalDynamicSessionLocked(session)
	if session.jobID != "" && s.hls != nil {
		_ = s.hls.manager.CancelJob(session.jobID, session.scope)
	}
	if session.input != nil {
		_ = session.input.Close()
		session.input = nil
	}
	if s.dynamicStreams != nil && s.dynamicStreams.store != nil {
		_ = s.dynamicStreams.store.ClosePresentation(context.Background(), timeshiftScope(session.scope), session.windowID)
	}
}

// A burn/profile change starts a new presentation. Previously retained output
// remains readable with its original pixels and subtitle view until expiration.
func (s *Server) stopDynamicProducer(session *dynamicStreamSession) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed || session.producerEnded {
		return
	}
	session.producerEnded = true
	if session.subtitles != nil {
		session.subtitles.StopEpoch(session.generation)
	}
	if session.jobID != "" && s.hls != nil {
		_ = s.hls.manager.CancelJob(session.jobID, session.scope)
	}
	if session.input != nil {
		_ = session.input.Close()
		session.input = nil
	}
	if s.dynamicStreams.store != nil {
		_ = s.dynamicStreams.store.SetState(context.Background(), timeshiftScope(session.scope), session.windowID, timeshift.State{Ended: true})
	}
	signalDynamicSessionLocked(session)
}

// cancelDynamicStreams uses the same authenticated credential/play scope as
// original and HLS cancellation. Empty playID cancels all credential plays.
func (s *Server) cancelDynamicStreams(authID, playID string) {
	if s.dynamicStreams == nil {
		s.dynamicSources.CancelMatching(authID, playID)
		return
	}
	s.dynamicStreams.mu.Lock()
	for id, session := range s.dynamicStreams.sessions {
		if session.scope.AuthSessionID == authID && (playID == "" || session.scope.PlaySessionID == playID) {
			s.retireDynamicSession(session)
			delete(s.dynamicStreams.sessions, id)
			if s.dynamicStreams.byKey[session.key] == session {
				delete(s.dynamicStreams.byKey, session.key)
			}
		}
	}
	s.dynamicStreams.mu.Unlock()
	s.dynamicSources.CancelMatching(authID, playID)
}

func (s *Server) cancelDynamicLease(owner dynamicsource.Owner, liveID string) {
	if s.dynamicStreams == nil {
		return
	}
	s.dynamicStreams.mu.Lock()
	var scopes []transcode.Scope
	for id, session := range s.dynamicStreams.sessions {
		if session.key.owner == owner.Identity() && session.key.liveID == liveID {
			s.retireDynamicSession(session)
			scopes = append(scopes, session.scope)
			delete(s.dynamicStreams.sessions, id)
			if s.dynamicStreams.byKey[session.key] == session {
				delete(s.dynamicStreams.byKey, session.key)
			}
		}
	}
	s.dynamicStreams.mu.Unlock()
	for _, scope := range scopes {
		s.releaseMediaPolicy(scope)
	}
}

func (s *Server) closeDynamicSources(ctx context.Context) error {
	if s.dynamicStreams == nil {
		return s.dynamicSources.Close(ctx)
	}
	s.dynamicStreams.mu.Lock()
	s.dynamicStreams.closing = true
	for _, session := range s.dynamicStreams.sessions {
		s.retireDynamicSession(session)
	}
	s.dynamicStreams.sessions = make(map[string]*dynamicStreamSession)
	s.dynamicStreams.byKey = make(map[dynamicStreamKey]*dynamicStreamSession)
	s.dynamicStreams.mu.Unlock()
	err := s.dynamicSources.Close(ctx)
	done := make(chan struct{})
	go func() { s.dynamicStreams.workers.Wait(); close(done) }()
	select {
	case <-ctx.Done():
		return errors.Join(err, ctx.Err())
	case <-done:
	}
	if s.dynamicStreams.store != nil {
		err = errors.Join(err, s.dynamicStreams.store.Close(ctx))
	}
	return err
}
