package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moooyo/goby/internal/dynamicsource"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
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
	revalidate func(context.Context, identity.Principal) (identity.Principal, error)
}

func (s *Server) initializeDynamicSources(_ context.Context) error {
	definitions := make([]dynamicsource.Definition, 0, len(s.cfg.DynamicSources))
	for _, configured := range s.cfg.DynamicSources {
		definitions = append(definitions, dynamicsource.Definition{ItemID: configured.ItemID, Name: configured.Name,
			URL: configured.URL, Headers: configured.Headers, Infinite: configured.Infinite, MaxReconnects: configured.MaxReconnects})
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
	conversion, err := playback.PlanDynamicConversion(dynamicPlaybackSource(lease), request, limits)
	if err != nil {
		return nil, err
	}
	if request.DeviceProfile == nil {
		return dynamicSourceDTO(lease), nil
	}
	if s.hls == nil || !s.hls.health().Available {
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
	key := dynamicStreamKey{owner: dynamicSourceOwner(principal).Identity(), liveID: lease.ID, plan: *conversion.Plan}
	runtime := s.dynamicStreams
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.closing {
		return nil, dynamicsource.ErrClosed
	}
	for id, previous := range runtime.sessions {
		previous.mu.Lock()
		expired := previous.closed || time.Since(previous.accessed) >= hlsIdleTTL ||
			previous.key.owner == key.owner && previous.key.liveID == key.liveID && previous.key.plan != key.plan
		if previous.jobID != "" {
			record, err := s.hls.manager.Snapshot(previous.scope, previous.jobID)
			expired = expired || err != nil || record.State == "failed" || record.State == "cancelled" || record.State == "interrupted" || previous.infinite && record.State == "completed"
		}
		previous.mu.Unlock()
		if expired {
			s.retireDynamicSession(previous)
			delete(runtime.sessions, id)
			delete(runtime.byKey, previous.key)
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
			accessed: time.Now(), presenceUpdated: time.Now(), ctx: lifetime, cancel: cancel, scope: transcode.Scope{UserID: principal.User.ID, AuthSessionID: principal.SessionID,
				DeviceID: principal.Client.DeviceID, ApplicationKey: principal.IsApplicationKey(), ApplicationClientID: principal.ClientSessionID,
				PlaySessionID: lease.PlaySessionID, ItemID: lease.ItemID, SourceID: lease.SourceID}}
		runtime.sessions[session.id], runtime.byKey[key] = session, session
	}
	token, _, _ := parseEmbyCredentials(r)
	dto := dynamicSourceDTO(lease)
	dto["TranscodingContainer"], dto["TranscodingSubProtocol"] = conversion.Plan.Container, "hls"
	if conversion.Method == "DirectStream" && request.EnableTranscoding != nil && !*request.EnableTranscoding {
		dto["SupportsDirectStream"], dto["DirectStreamUrl"] = true, dynamicArtifactURL(session, "master.m3u8", token)
	} else {
		dto["SupportsTranscoding"], dto["TranscodingUrl"] = true, dynamicArtifactURL(session, "master.m3u8", token)
	}
	if conversion.Output.DefaultAudioStreamIndex != nil {
		dto["DefaultAudioStreamIndex"] = *conversion.Output.DefaultAudioStreamIndex
	}
	dto["DefaultSubtitleStreamIndex"] = -1
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
	query := url.Values{"api_key": {token}, "GobyLiveId": {session.id}, "PlaySessionId": {session.scope.PlaySessionID},
		"DeviceId": {session.scope.DeviceID}, "MediaSourceId": {session.scope.SourceID}}
	return "/emby/LiveStreams/" + url.PathEscape(session.key.liveID) + "/hls/" + url.PathEscape(name) + "?" + query.Encode()
}

func (s *Server) dynamicMediaInfoDTO(r *http.Request, principal identity.Principal, lease dynamicsource.Lease) (map[string]any, error) {
	if s.dynamicStreams == nil {
		return dynamicSourceDTO(lease), nil
	}
	s.dynamicStreams.mu.Lock()
	var selected *dynamicStreamSession
	for _, session := range s.dynamicStreams.sessions {
		if session.key.owner == dynamicSourceOwner(principal).Identity() && session.key.liveID == lease.ID {
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
		case "gobyliveid", "playsessionid", "mediasourceid", "deviceid", "api_key", "starttimeticks":
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
	if start := values["starttimeticks"]; start != "" && start != "0" {
		return nil, dynamicsource.Lease{}, dynamicsource.ErrInvalid
	}
	if err := s.checkMediaPolicy(principal, session.scope); err != nil {
		s.retireDynamicSession(session)
		return nil, dynamicsource.Lease{}, err
	}
	lease, err := s.dynamicSources.Info(r.Context(), dynamicSourceOwner(principal), session.key.liveID)
	if err != nil {
		s.retireDynamicSession(session)
		return nil, lease, err
	}
	limits, err := s.dynamicLimits(r.Context(), principal, session.request, r)
	if err != nil {
		s.retireDynamicSession(session)
		return nil, lease, err
	}
	conversion, err := playback.PlanDynamicConversion(dynamicPlaybackSource(lease), session.request, limits)
	if err != nil || conversion.Plan == nil || *conversion.Plan != session.key.plan || !principalPlanBitrateAllowed(principal, library.MediaFile{Item: library.Item{Media: &lease.Info}}, session.key.plan) {
		s.retireDynamicSession(session)
		return nil, lease, library.ErrForbidden
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
	if session.jobID != "" {
		record, err := s.hls.manager.Snapshot(session.scope, session.jobID)
		if err == nil && (record.State == "queued" || record.State == "running" || record.State == "completed" && !session.infinite) {
			return record.ID, nil
		}
		if session.input != nil {
			_ = session.input.Close()
			session.input = nil
		}
		// A new upstream presentation must receive a fresh public output ID.
		// Close this revision; Open/MediaInfo negotiation can create the next
		// bounded reconnect without reusing old segment URLs or media sequence.
		session.closed = true
		session.cancel()
		_ = s.hls.manager.CancelJob(session.jobID, session.scope)
		return "", dynamicsource.ErrUnavailable
	}
	if r.Method == http.MethodHead {
		return "", dynamicsource.ErrNotFound
	}
	input, err := s.dynamicSources.Acquire(ctx, dynamicSourceOwner(r.Context().Value(principalKey).(identity.Principal)), session.key.liveID)
	if err != nil {
		return "", err
	}
	principal, err := s.freshDynamicPrincipal(ctx, r.Context().Value(principalKey).(identity.Principal))
	if err != nil {
		_ = input.Close()
		return "", err
	}
	limits, err := s.dynamicLimits(ctx, principal, session.request, r)
	if err != nil {
		_ = input.Close()
		return "", err
	}
	conversion, err := playback.PlanDynamicConversion(dynamicPlaybackSource(input.Lease), session.request, limits)
	if err != nil || conversion.Plan == nil || *conversion.Plan != session.key.plan || !principalPlanBitrateAllowed(principal, library.MediaFile{Item: library.Item{Media: &input.Info}}, session.key.plan) {
		_ = input.Close()
		return "", errHLSRequestUnsupported
	}
	pipe, err := input.OpenPipe()
	if err != nil {
		_ = input.Close()
		return "", err
	}
	record, err := s.hls.manager.Ensure(ctx, transcode.Spec{Scope: session.scope, SourceStamp: input.Stamp, Plan: session.key.plan}, pipe)
	if err != nil {
		_ = input.Close()
		return "", err
	}
	session.input, session.jobID, session.output = input, record.ID, conversion.OutputSource
	return record.ID, nil
}

func dynamicMasterPlaylist(session *dynamicStreamSession, token string) ([]byte, error) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.closed {
		return nil, dynamicsource.ErrNotFound
	}
	plan := session.key.plan
	var result strings.Builder
	result.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n")
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
		child := dynamicArtifactURL(session, transcode.HLSPlaylistName(index, plan.HLS.RenditionCount), token)
		if bandwidth <= 0 || !validHLSManifestURL(child) {
			return nil, errInvalidHLSManifest
		}
		fmt.Fprintf(&result, "#EXT-X-STREAM-INF:BANDWIDTH=%d", bandwidth)
		if width > 0 && height > 0 {
			fmt.Fprintf(&result, ",RESOLUTION=%dx%d", width, height)
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
		s.liveStreamError(w, r, err)
		return
	}
	work, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(session.ctx, cancel)
	defer func() { stop(); cancel() }()
	token, _, _ := parseEmbyCredentials(r)
	name := r.PathValue("Artifact")
	policyAdmitted, delivered := false, false
	defer func() {
		if policyAdmitted && !delivered {
			s.failMediaPolicy(work, session.scope)
		} else if policyAdmitted && work.Err() == nil {
			s.touchMediaPolicy(work, principal, session.scope)
		}
	}()
	var body []byte
	if name == "master.m3u8" {
		body, err = dynamicMasterPlaylist(session, token)
	} else {
		if !hlsPlanArtifact(session.key.plan, name) {
			s.liveStreamError(w, r, dynamicsource.ErrNotFound)
			return
		}
		if r.Method != http.MethodHead {
			fresh, freshErr := s.freshDynamicPrincipal(work, principal)
			if freshErr != nil {
				s.identityError(w, r, freshErr)
				return
			}
			policyContext, release, policyErr := s.acquireMediaPolicy(work, fresh, session.scope)
			if policyErr != nil {
				s.hlsError(w, r, policyErr)
				return
			}
			defer release()
			policyAdmitted = true
			work = policyContext
			r = r.WithContext(context.WithValue(work, principalKey, fresh))
		}
		jobID, jobErr := s.ensureDynamicJob(work, r, session)
		if jobErr != nil {
			s.liveStreamError(w, r, jobErr)
			return
		}
		var handle *transcode.ReadHandle
		if r.Method == http.MethodHead {
			handle, err = s.hls.manager.TryOpen(session.scope, jobID, name)
		} else {
			handle, err = s.hls.manager.Open(work, session.scope, jobID, name)
		}
		if err != nil {
			s.hlsError(w, r, err)
			return
		}
		defer handle.Close()
		if strings.HasSuffix(name, ".m3u8") {
			body, err = io.ReadAll(io.LimitReader(handle, transcode.MaxPlaylistBytes+1))
			if err == nil {
				prefix := ""
				if name != "main.m3u8" {
					prefix = strings.TrimSuffix(name, ".m3u8") + "-"
				}
				body, err = transcode.RewriteMediaPlaylistWithMap(body, func(segment transcode.MediaSegment) string {
					if !hlsPlanArtifact(session.key.plan, segment.Name) || !strings.HasPrefix(segment.Name, prefix+"segment-") {
						return ""
					}
					return dynamicArtifactURL(session, segment.Name, token)
				}, func(init string) string {
					if !hlsPlanArtifact(session.key.plan, init) || init != prefix+"init.mp4" {
						return ""
					}
					return dynamicArtifactURL(session, init, token)
				})
			}
		} else {
			if _, _, err := s.findDynamicSession(r, values); err != nil {
				s.liveStreamError(w, r, err)
				return
			}
			info, err := handle.Stat()
			if err != nil {
				s.hlsError(w, r, err)
				return
			}
			contentType := "video/mp2t"
			if strings.HasSuffix(name, ".mp4") || strings.HasSuffix(name, ".m4s") {
				contentType = "video/mp4"
				if session.key.plan.VideoStreamIndex < 0 {
					contentType = "audio/mp4"
				}
			}
			digest := sha256.Sum256([]byte(jobID + ":" + name + ":" + strconv.FormatInt(info.Size(), 10)))
			w.Header().Set("Content-Type", contentType)
			w.Header().Set("Cache-Control", "private, no-transform")
			w.Header().Set("ETag", "\""+hex.EncodeToString(digest[:])+"\"")
			stopRead := context.AfterFunc(work, func() { _ = handle.Close() })
			delivered = true
			http.ServeContent(w, r, name, info.ModTime(), handle)
			stopRead()
			return
		}
	}
	if err != nil {
		s.hlsError(w, r, err)
		return
	}
	if len(body) > maxHLSManifestBytes {
		s.hlsError(w, r, errHLSManifestLimit)
		return
	}
	if _, _, err := s.findDynamicSession(r, values); err != nil {
		s.liveStreamError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	delivered = true
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
	if session.input != nil {
		_ = session.input.Close()
		session.input = nil
	}
	if session.jobID != "" && s.hls != nil {
		_ = s.hls.manager.CancelJob(session.jobID, session.scope)
	}
}

// cancelDynamicStreams uses the same authenticated credential/play scope as
// original and HLS cancellation. Empty playID cancels all credential plays.
func (s *Server) cancelDynamicStreams(authID, playID string) {
	s.dynamicSources.CancelMatching(authID, playID)
	if s.dynamicStreams == nil {
		return
	}
	s.dynamicStreams.mu.Lock()
	defer s.dynamicStreams.mu.Unlock()
	for id, session := range s.dynamicStreams.sessions {
		if session.scope.AuthSessionID == authID && (playID == "" || session.scope.PlaySessionID == playID) {
			s.retireDynamicSession(session)
			delete(s.dynamicStreams.sessions, id)
			delete(s.dynamicStreams.byKey, session.key)
		}
	}
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
			delete(s.dynamicStreams.byKey, session.key)
		}
	}
	s.dynamicStreams.mu.Unlock()
	for _, scope := range scopes {
		s.releaseMediaPolicy(scope)
	}
}

func (s *Server) closeDynamicSources(ctx context.Context) error {
	err := s.dynamicSources.Close(ctx)
	if s.dynamicStreams == nil {
		return err
	}
	s.dynamicStreams.mu.Lock()
	s.dynamicStreams.closing = true
	for _, session := range s.dynamicStreams.sessions {
		s.retireDynamicSession(session)
	}
	s.dynamicStreams.sessions = make(map[string]*dynamicStreamSession)
	s.dynamicStreams.byKey = make(map[dynamicStreamKey]*dynamicStreamSession)
	s.dynamicStreams.mu.Unlock()
	return err
}
