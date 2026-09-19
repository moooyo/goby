package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/dynamicsource"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

func (s *Server) registerLiveStreamRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /emby/LiveStreams/Open", s.requireEmby(s.openLiveStream))
	mux.HandleFunc("POST /emby/LiveStreams/MediaInfo", s.requireEmby(s.liveStreamMediaInfo))
	mux.HandleFunc("POST /emby/LiveStreams/Close", s.requireEmby(s.closeLiveStream))
	mux.HandleFunc("GET /emby/LiveStreams/{LiveStreamId}/hls/{Artifact}", s.requireEmby(s.dynamicHLSArtifact))
}

func dynamicSourceOwner(principal identity.Principal) dynamicsource.Owner {
	return dynamicsource.Owner{UserID: principal.User.ID, SessionID: principal.SessionID, DeviceID: principal.Client.DeviceID,
		PeerIP: principal.PeerIP, ApplicationKey: principal.IsApplicationKey(), ApplicationClientID: principal.ClientSessionID}
}

type liveStreamRequest struct {
	OpenToken, ItemID, PlaySessionID string
	Playback                         playback.Request
}

// ItemId is an int64 in the pinned request, while catalog IDs and the response
// ItemId are strings. Accept exact decimal integers and string IDs without a
// float64 round trip. Profile normalization uses the established bounded wire
// adapter; opening metadata cannot supply a source URL or alter ownership.
func parseLiveStreamRequest(data []byte) (liveStreamRequest, error) {
	request, err := parsePlaybackInfoBody(data)
	if err != nil {
		return liveStreamRequest{}, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return liveStreamRequest{}, playback.ErrInvalidRequest
	}
	var result liveStreamRequest
	for name, value := range raw {
		switch strings.ToLower(name) {
		case "opentoken":
			if json.Unmarshal(value, &result.OpenToken) != nil {
				return liveStreamRequest{}, playback.ErrInvalidRequest
			}
		case "playsessionid":
			if json.Unmarshal(value, &result.PlaySessionID) != nil {
				return liveStreamRequest{}, playback.ErrInvalidRequest
			}
		case "itemid":
			if len(value) == 0 {
				return liveStreamRequest{}, playback.ErrInvalidRequest
			}
			if value[0] == '"' {
				if json.Unmarshal(value, &result.ItemID) != nil {
					return liveStreamRequest{}, playback.ErrInvalidRequest
				}
			} else {
				id, err := strconv.ParseInt(string(value), 10, 64)
				if err != nil || id <= 0 {
					return liveStreamRequest{}, playback.ErrInvalidRequest
				}
				result.ItemID = strconv.FormatInt(id, 10)
			}
		case "path", "url", "encoderpath", "probepath", "requiredhttpheaders":
			return liveStreamRequest{}, playback.ErrInvalidRequest
		}
	}
	if result.OpenToken == "" || len(result.OpenToken) > 256 || len(result.ItemID) > 256 || len(result.PlaySessionID) > 256 ||
		request.ID != "" && request.ID != result.ItemID || request.CurrentPlaySessionID != "" && request.CurrentPlaySessionID != result.PlaySessionID || request.LiveStreamID != "" {
		return liveStreamRequest{}, playback.ErrInvalidRequest
	}
	request.ID, request.CurrentPlaySessionID = result.ItemID, result.PlaySessionID
	result.Playback = request
	return result, nil
}

func (s *Server) openLiveStream(w http.ResponseWriter, r *http.Request) {
	var raw json.RawMessage
	if !decodeEmbyJSONBody(w, r, &raw) {
		return
	}
	request, err := parseLiveStreamRequest(raw)
	if err != nil {
		s.liveStreamError(w, r, dynamicsource.ErrInvalid)
		return
	}
	principal := r.Context().Value(principalKey).(identity.Principal)
	principal, err = s.bindKeyPlaybackContext(r, principal, request.PlaySessionID)
	if err != nil {
		s.identityError(w, r, err)
		return
	}
	r = r.WithContext(context.WithValue(r.Context(), principalKey, principal))
	if !principal.IsApplicationKey() && request.Playback.UserID != "" && request.Playback.UserID != principal.User.ID {
		apiError(w, r, http.StatusForbidden, "access_denied", "The source lease belongs to the authenticated user.")
		return
	}
	values, err := streamValues(r)
	if err != nil {
		s.liveStreamError(w, r, dynamicsource.ErrInvalid)
		return
	}
	if deviceID := values["deviceid"]; deviceID != "" && deviceID != principal.Client.DeviceID {
		apiError(w, r, http.StatusForbidden, "device_mismatch", "The source lease belongs to the authenticated device.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	owner := dynamicSourceOwner(principal)
	description, err := s.dynamicSources.DescribeToken(ctx, owner, request.OpenToken, request.ItemID)
	if err != nil {
		s.liveStreamError(w, r, err)
		return
	}
	if request.Playback.MediaSourceID != "" && request.Playback.MediaSourceID != description.SourceID {
		s.liveStreamError(w, r, dynamicsource.ErrNotFound)
		return
	}
	play, err := s.library.PrepareDynamicPlayback(ctx, playbackOwner(principal), description.ItemID, description.SourceID, request.PlaySessionID)
	if err != nil {
		s.playbackError(w, r, err)
		return
	}
	lease, err := s.dynamicSources.Open(ctx, owner, dynamicsource.OpenRequest{ItemID: description.ItemID, OpenToken: request.OpenToken, PlaySessionID: play.ID})
	if err != nil {
		s.liveStreamError(w, r, err)
		return
	}
	request.Playback.ID, request.Playback.MediaSourceID, request.Playback.CurrentPlaySessionID = lease.ItemID, lease.SourceID, lease.PlaySessionID
	dto, err := s.dynamicPlaybackDTO(r, principal, lease, request.Playback)
	if err != nil {
		_ = s.dynamicSources.CloseLease(context.Background(), owner, lease.ID)
		s.liveStreamError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, map[string]any{"MediaSource": dto})
}

func liveStreamID(r *http.Request) (string, error) {
	values, err := streamValues(r)
	if err != nil || values["livestreamid"] == "" || len(values["livestreamid"]) > 256 {
		return "", dynamicsource.ErrInvalid
	}
	return values["livestreamid"], nil
}

func (s *Server) bindLiveStreamPrincipal(r *http.Request, principal identity.Principal, liveID string) (identity.Principal, error) {
	if !principal.IsApplicationKey() {
		return principal, nil
	}
	playID, err := s.dynamicSources.PlaybackReference(dynamicSourceOwner(principal), liveID)
	if err != nil {
		return identity.Principal{}, err
	}
	return s.bindKeyPlaybackContext(r, principal, playID)
}

func (s *Server) liveStreamMediaInfo(w http.ResponseWriter, r *http.Request) {
	id, err := liveStreamID(r)
	if err != nil {
		s.liveStreamError(w, r, err)
		return
	}
	principal := r.Context().Value(principalKey).(identity.Principal)
	principal, err = s.bindLiveStreamPrincipal(r, principal, id)
	if err != nil {
		s.liveStreamError(w, r, err)
		return
	}
	r = r.WithContext(context.WithValue(r.Context(), principalKey, principal))
	lease, err := s.dynamicSources.Info(r.Context(), dynamicSourceOwner(principal), id)
	if err != nil {
		s.liveStreamError(w, r, err)
		return
	}
	// The pinned SDK leaves this response untyped. Goby returns the current
	// MediaSourceInfo projection, preserving source and lease identities.
	dto, err := s.dynamicMediaInfoDTO(r, principal, lease)
	if err != nil {
		s.liveStreamError(w, r, err)
		return
	}
	jsonResponse(w, http.StatusOK, dto)
}

func (s *Server) closeLiveStream(w http.ResponseWriter, r *http.Request) {
	id, err := liveStreamID(r)
	if err != nil {
		s.liveStreamError(w, r, err)
		return
	}
	principal := r.Context().Value(principalKey).(identity.Principal)
	principal, err = s.bindLiveStreamPrincipal(r, principal, id)
	if err != nil {
		s.liveStreamError(w, r, err)
		return
	}
	r = r.WithContext(context.WithValue(r.Context(), principalKey, principal))
	owner := dynamicSourceOwner(principal)
	lease, _ := s.dynamicSources.Info(r.Context(), owner, id)
	s.cancelDynamicLease(owner, id)
	if err := s.dynamicSources.CloseLease(r.Context(), owner, id); err != nil {
		s.liveStreamError(w, r, err)
		return
	}
	if lease.PlaySessionID != "" {
		s.cancelPlaybackResources(principal.SessionID, lease.PlaySessionID)
	}
	w.WriteHeader(http.StatusOK)
}

func describeDynamicSourceDTO(description dynamicsource.Description) map[string]any {
	return map[string]any{"Id": description.SourceID, "ItemId": description.ItemID, "Name": description.Name,
		"Protocol": "Http", "Type": "Default", "IsRemote": true, "IsInfiniteStream": description.Infinite,
		"RequiresOpening": true, "RequiresClosing": false, "OpenToken": description.OpenToken,
		"SupportsDirectPlay": false, "SupportsDirectStream": false, "SupportsTranscoding": false,
		"SupportsProbing": true, "RequiresLooping": false, "MediaStreams": []map[string]any{}, "RequiredHttpHeaders": map[string]string{}}
}

func dynamicSourceDTO(lease dynamicsource.Lease) map[string]any {
	dto := describeDynamicSourceDTO(lease.Description)
	dto["RequiresOpening"], dto["RequiresClosing"], dto["LiveStreamId"] = false, true, lease.ID
	dto["Container"] = media.CanonicalContainer(lease.Info, "")
	dto["MediaStreams"] = mediaStreamsDTO(lease.Info.Streams)
	dto["Bitrate"] = lease.Info.Bitrate
	if lease.Info.DurationTicks > 0 && !lease.Infinite {
		dto["RunTimeTicks"] = lease.Info.DurationTicks
	}
	if lease.Info.Size > 0 {
		dto["Size"] = lease.Info.Size
	}
	delete(dto, "OpenToken")
	return dto
}

func (s *Server) liveStreamError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, context.Canceled):
		return
	case errors.Is(err, identity.ErrUnauthorized):
		s.identityError(w, r, err)
	case errors.Is(err, dynamicsource.ErrInvalid), errors.Is(err, playback.ErrInvalidRequest):
		apiError(w, r, http.StatusBadRequest, "invalid_live_stream_request", "Supply a registered source opening token and matching playback identifiers.")
	case errors.Is(err, errHLSRequestUnsupported):
		apiError(w, r, http.StatusUnsupportedMediaType, "NoCompatibleStream", "No authorized HLS output supports this nonseekable source and request.")
	case errors.Is(err, transcode.ErrInvalidPlan), errors.Is(err, transcode.ErrBusy), errors.Is(err, transcode.ErrQuota), errors.Is(err, transcode.ErrJobNotFound), errors.Is(err, transcode.ErrJobCancelled):
		s.hlsError(w, r, err)
	case errors.Is(err, dynamicsource.ErrNotFound):
		apiError(w, r, http.StatusNotFound, "not_found", "The dynamic source or lease was not found.")
	case errors.Is(err, dynamicsource.ErrBusy):
		w.Header().Set("Retry-After", "2")
		apiError(w, r, http.StatusTooManyRequests, "live_stream_limit", "The dynamic source capacity is exhausted.")
	case errors.Is(err, dynamicsource.ErrUnavailable), errors.Is(err, dynamicsource.ErrClosed), errors.Is(err, context.DeadlineExceeded):
		apiError(w, r, http.StatusServiceUnavailable, "live_stream_unavailable", "The configured source could not be opened within its limits.")
	default:
		s.playbackError(w, r, err)
	}
}
