package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

func (s *Server) videoStream(w http.ResponseWriter, r *http.Request) {
	container, original, valid := streamAlias(r.PathValue("StreamFileName"))
	if !valid {
		apiError(w, r, http.StatusNotFound, "not_found", "The requested video route is not available.")
		return
	}
	values, err := hlsValues(r)
	if err != nil {
		s.videoError(w, r, errVideoRequestInvalid)
		return
	}
	if original {
		if value, exists := values["static"]; exists {
			static, err := strconv.ParseBool(value)
			if err != nil || !static {
				s.videoError(w, r, errVideoRequestInvalid)
				return
			}
		}
		values["static"] = "true"
	}
	principal := r.Context().Value(principalKey).(identity.Principal)
	if !principal.IsApplicationKey() && (values["userid"] != "" && values["userid"] != principal.User.ID || values["deviceid"] != "" && values["deviceid"] != principal.Client.DeviceID) {
		s.videoError(w, r, library.ErrForbidden)
		return
	}
	select {
	case s.streamSlots <- struct{}{}:
		defer func() { <-s.streamSlots }()
	default:
		w.Header().Set("Retry-After", "2")
		apiError(w, r, http.StatusTooManyRequests, "stream_limit", "The active media stream limit has been reached.")
		return
	}
	prepare, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	file, source, err := s.library.OpenMediaFor(prepare, librarySubject(principal, principal.User.ID), r.PathValue("Id"), values["mediasourceid"])
	if err != nil {
		s.videoError(w, r, err)
		return
	}
	defer func() {
		if file != nil {
			_ = file.Close()
		}
	}()
	if strings.EqualFold(source.Item.Type, "Audio") {
		s.videoError(w, r, library.ErrNotFound)
		return
	}
	input := playback.Source{ItemID: source.Item.ID, MediaSourceID: source.SourceID,
		Path: source.Item.Path, ItemType: source.Item.Type, Info: playbackMediaInfo(source.Item)}
	planning := s.requestPlanningConfig(r)
	decision, err := videoRequestDecision(input, values, container, hlsPrincipalLimits(planning, principal))
	if err != nil {
		s.videoError(w, r, err)
		return
	}
	if decision.Original {
		s.serveOriginalMedia(w, r, file, source)
		return
	}
	if s.hls == nil || decision.Conversion.Plan == nil {
		s.videoError(w, r, errVideoRequestUnsupported)
		return
	}
	var play library.PlaySession
	if reference := values["playsessionid"]; reference != "" {
		play, err = s.library.PrepareCorrelatedPlayback(prepare, playbackOwner(principal), source.Item.ID, source.SourceID, reference)
	} else {
		play, err = s.library.PreparePlayback(prepare, playbackOwner(principal), source.Item.ID, source.SourceID, "")
	}
	if err != nil {
		s.videoError(w, r, err)
		return
	}
	session, err := s.hls.register(principal, source, play.ID, decision.Conversion, decision.StartTicks)
	if err != nil {
		s.videoError(w, r, err)
		return
	}
	owned := file
	file = nil
	s.serveProgressiveMedia(w, r, session, owned)
}

func videoPlaybackURL(itemID, sourceID, playID, deviceID, token string, plan transcode.Plan) string {
	query := url.Values{"DeviceId": {deviceID}, "MediaSourceId": {sourceID}, "PlaySessionId": {playID}, "api_key": {token},
		"Static": {"false"}, "StartTimeTicks": {strconv.FormatInt(plan.StartTicks, 10)},
		"VideoStreamIndex": {strconv.Itoa(plan.VideoStreamIndex)}, "VideoCodec": {plan.VideoCodec},
		"AllowVideoStreamCopy": {strconv.FormatBool(plan.VideoCodec == "copy")},
		"SubtitleStreamIndex":  {"-1"}}
	if plan.VideoCodec != "copy" {
		query.Set("Width", strconv.Itoa(plan.Width))
		query.Set("Height", strconv.Itoa(plan.Height))
		query.Set("VideoBitrate", strconv.FormatInt(plan.VideoBitrate, 10))
		if plan.FrameRate > 0 {
			query.Set("Framerate", strconv.FormatFloat(plan.FrameRate, 'f', -1, 64))
		}
	}
	if plan.AudioStreamIndex >= 0 {
		query.Set("AudioStreamIndex", strconv.Itoa(plan.AudioStreamIndex))
		query.Set("AudioCodec", plan.AudioCodec)
		query.Set("AllowAudioStreamCopy", strconv.FormatBool(plan.AudioCodec == "copy"))
		if plan.AudioCodec != "copy" {
			query.Set("AudioBitrate", strconv.FormatInt(plan.AudioBitrate, 10))
			query.Set("AudioChannels", strconv.Itoa(plan.AudioChannels))
			query.Set("AudioSampleRate", strconv.Itoa(plan.AudioSampleRate))
		}
	}
	return "/emby/Videos/" + url.PathEscape(itemID) + "/stream.mp4?" + query.Encode()
}

func (s *Server) videoError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, context.Canceled):
		if r.Context().Err() == nil {
			apiError(w, r, http.StatusNotFound, "video_cancelled", "The video output was cancelled.")
		}
	case errors.Is(err, identity.ErrUnauthorized):
		s.identityError(w, r, err)
	case errors.Is(err, errVideoRequestInvalid), errors.Is(err, playback.ErrInvalidRequest), errors.Is(err, library.ErrInvalidInput):
		apiError(w, r, http.StatusBadRequest, "invalid_video_request", "Check video parameters and stream identifiers.")
	case errors.Is(err, errVideoRequestUnsupported), errors.Is(err, transcode.ErrInvalidPlan), errors.Is(err, transcode.ErrUnsupportedTimeline):
		apiError(w, r, http.StatusUnsupportedMediaType, "NoCompatibleStream", "This source does not support the requested video output.")
	case errors.Is(err, transcode.ErrBusy), errors.Is(err, transcode.ErrQuota), errors.Is(err, library.ErrBusy):
		w.Header().Set("Retry-After", "2")
		apiError(w, r, http.StatusTooManyRequests, "RateLimitExceeded", "Video playback capacity is unavailable.")
	case errors.Is(err, library.ErrForbidden):
		apiError(w, r, http.StatusForbidden, "playback_denied", "The requested playback is not permitted.")
	case errors.Is(err, library.ErrNotFound):
		s.libraryError(w, r, err)
	case errors.Is(err, transcode.ErrJobNotFound), errors.Is(err, transcode.ErrJobCancelled):
		apiError(w, r, http.StatusNotFound, "video_not_found", "The video resource is no longer available.")
	default:
		apiError(w, r, http.StatusServiceUnavailable, "video_unavailable", "The video output could not be prepared.")
	}
}
