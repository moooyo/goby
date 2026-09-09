package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

func (s *Server) registerHLSRoutes(mux *http.ServeMux) {
	for _, kind := range []string{"Videos", "Audio"} {
		mux.HandleFunc("GET /emby/"+kind+"/{Id}/master.m3u8", s.requireEmby(s.hlsPlaylist(kind == "Audio", true)))
		mux.HandleFunc("GET /emby/"+kind+"/{Id}/main.m3u8", s.requireEmby(s.hlsPlaylist(kind == "Audio", false)))
		mux.HandleFunc("GET /emby/"+kind+"/{Id}/hls1/{PlaylistId}/{SegmentFile}", s.requireEmby(s.hlsSegment(kind == "Audio")))
	}
	mux.HandleFunc("DELETE /emby/Videos/ActiveEncodings", s.requireEmby(s.stopHLSEncodings))
	mux.HandleFunc("POST /emby/Videos/ActiveEncodings/Delete", s.requireEmby(s.stopHLSEncodings))
}

func hlsSessionURL(session *hlsSession, resource, file, token string, start int64) string {
	values := url.Values{"GobyHlsId": {session.id}, "PlaySessionId": {session.key.scope.PlaySessionID},
		"MediaSourceId": {session.key.scope.SourceID}, "DeviceId": {session.key.scope.DeviceID}, "api_key": {token}}
	values.Set("StartTimeTicks", strconv.FormatInt(start, 10))
	return "/emby/" + resource + "/" + url.PathEscape(session.key.scope.ItemID) + "/" + file + "?" + values.Encode()
}

func (s *Server) beginHLS(w http.ResponseWriter, r *http.Request) (context.Context, func(), bool) {
	if s.hls == nil || !s.hls.enter() {
		apiError(w, r, http.StatusServiceUnavailable, "conversion_unavailable", "HLS conversion is unavailable.")
		return nil, nil, false
	}
	select {
	case s.hls.slots <- struct{}{}:
	default:
		s.hls.requests.Done()
		w.Header().Set("Retry-After", "2")
		apiError(w, r, http.StatusTooManyRequests, "hls_request_limit", "The HLS request limit has been reached.")
		return nil, nil, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	stop := context.AfterFunc(s.hls.ctx, cancel)
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Now().Add(2 * time.Minute))
	interrupted := make(chan struct{})
	stopTransport := context.AfterFunc(ctx, func() {
		_ = controller.SetWriteDeadline(time.Now())
		close(interrupted)
	})
	return ctx, func() {
		if !stopTransport() {
			<-interrupted
		}
		_ = controller.SetWriteDeadline(time.Time{})
		stop()
		cancel()
		<-s.hls.slots
		s.hls.requests.Done()
	}, true
}

func hlsValues(r *http.Request) (map[string]string, error) {
	if len(r.URL.RawQuery) > 64*1024 {
		return nil, errHLSRequestInvalid
	}
	return streamValues(r)
}

func hlsStart(values map[string]string, fallback, duration int64) (int64, error) {
	if value, ok := values["starttimeticks"]; ok {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return 0, errHLSRequestInvalid
		}
		fallback = parsed
	}
	if fallback < 0 || fallback >= duration {
		return 0, errHLSRequestInvalid
	}
	return fallback, nil
}

func (s *Server) resolveHLS(ctx context.Context, r *http.Request, values map[string]string) (*hlsSession, *os.File, library.MediaFile, error) {
	principal := r.Context().Value(principalKey).(identity.Principal)
	if device := values["deviceid"]; device != "" && device != principal.Client.DeviceID {
		return nil, nil, library.MediaFile{}, library.ErrForbidden
	}
	if id := values["gobyhlsid"]; id != "" {
		session, err := s.hls.find(id, principal, r.PathValue("Id"))
		if err != nil {
			return nil, nil, library.MediaFile{}, err
		}
		if session.key.plan.OutputMode != "" {
			return nil, nil, library.MediaFile{}, library.ErrNotFound
		}
		if reference := values["playsessionid"]; reference != "" && reference != session.key.scope.PlaySessionID {
			canonical, err := s.library.ResolvePlaybackReference(ctx, playbackOwner(principal), reference)
			if err != nil {
				return nil, nil, library.MediaFile{}, err
			}
			if canonical != session.key.scope.PlaySessionID {
				return nil, nil, library.MediaFile{}, library.ErrNotFound
			}
		}
		if values["mediasourceid"] != "" && values["mediasourceid"] != session.key.scope.SourceID {
			return nil, nil, library.MediaFile{}, library.ErrNotFound
		}
		// A negotiated revision is immutable. Quality/track changes require a
		// fresh PlaybackInfo result instead of mutating an existing segment URL.
		for _, key := range []string{"videocodec", "audiocodec", "videobitrate", "audiobitrate", "width", "height", "maxwidth", "maxheight",
			"audiostreamindex", "subtitlestreamindex", "segmentlength", "allowvideostreamcopy", "allowaudiostreamcopy", "enableautostreamcopy",
			"audiochannels", "maxaudiochannels", "transcodingmaxaudiochannels", "audiosamplerate", "framerate", "maxframerate",
			"maxstreamingbitrate", "container", "segmentcontainer", "protocol", "copytimestamps", "minsegments", "breakonnonkeyframes", "subtitlemethod"} {
			if _, supplied := values[key]; supplied {
				return nil, nil, library.MediaFile{}, errHLSRequestInvalid
			}
		}
		file, source, err := s.authorizeHLS(ctx, principal, session.key.scope, session.key.stamp, session.key.plan)
		if permanentHLSError(err) {
			s.hls.retire(session)
		}
		return session, file, source, err
	}
	if values["playsessionid"] == "" {
		return nil, nil, library.MediaFile{}, errHLSRequestInvalid
	}
	play, err := s.library.GetPlaybackSession(ctx, playbackOwner(principal), values["playsessionid"])
	if err != nil {
		return nil, nil, library.MediaFile{}, err
	}
	if play.ItemID != r.PathValue("Id") || values["mediasourceid"] != "" && values["mediasourceid"] != play.MediaSourceID ||
		!time.Now().Before(play.ExpiresAt) || (play.State != "Prepared" && play.State != "Playing" && play.State != "Paused") {
		return nil, nil, library.MediaFile{}, library.ErrNotFound
	}
	file, source, err := s.library.OpenMedia(ctx, principal.User.ID, play.ItemID, play.MediaSourceID)
	if err != nil {
		return nil, nil, library.MediaFile{}, err
	}
	decision, err := hlsRequestConversion(values, playback.Source{ItemID: source.Item.ID, MediaSourceID: source.SourceID,
		Path: source.Item.Path, ItemType: source.Item.Type, Info: playbackMediaInfo(source.Item)}, hlsUserLimits(s.cfg.Transcoding, principal.User))
	if err != nil || decision.Plan == nil {
		_ = file.Close()
		if err == nil {
			err = errHLSRequestUnsupported
		}
		return nil, nil, library.MediaFile{}, err
	}
	if source.Item.Type == "Audio" && exactAudioCoverage(*source.Item.Media, decision.Plan.AudioStreamIndex) == nil {
		_ = file.Close()
		return nil, nil, library.MediaFile{}, errHLSRequestUnsupported
	}
	start, err := hlsStart(values, 0, source.Item.Media.DurationTicks)
	if err != nil {
		_ = file.Close()
		return nil, nil, library.MediaFile{}, err
	}
	session, err := s.hls.register(principal, source, play.ID, decision, start)
	if err != nil {
		_ = file.Close()
		return nil, nil, library.MediaFile{}, err
	}
	return session, file, source, nil
}

func (s *Server) hlsPlaylist(audioOnly, master bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, done, ok := s.beginHLS(w, r)
		if !ok {
			return
		}
		defer done()
		values, err := hlsValues(r)
		if err != nil {
			s.hlsError(w, r, errHLSRequestInvalid)
			return
		}
		session, file, source, err := s.resolveHLS(ctx, r, values)
		if err != nil {
			s.hlsError(w, r, err)
			return
		}
		defer file.Close()
		if (source.Item.Type == "Audio") != audioOnly {
			s.hlsError(w, r, library.ErrNotFound)
			return
		}
		startHint := session.startHint
		if values["gobyhlsid"] == "" {
			// An omitted start in a manual request means the source beginning,
			// even when registration reuses a revision first opened after a seek.
			startHint = 0
		}
		start, err := hlsStart(values, startHint, session.key.plan.DurationTicks)
		if err != nil {
			s.hlsError(w, r, err)
			return
		}
		token, _, _ := parseEmbyCredentials(r)
		resource := "Videos"
		if audioOnly {
			resource = "Audio"
		}
		var body []byte
		if master {
			var width, height int
			for _, stream := range session.output.Info.Streams {
				if stream.CodecType == "video" && !stream.IsAttachedPicture {
					width, height = stream.Width, stream.Height
					break
				}
			}
			body, err = hlsMasterPlaylist(hlsSessionURL(session, resource, "main.m3u8", token, start), session.output.Info.Bitrate, width, height)
		} else {
			var timeline transcode.Timeline
			timeline, err = s.hls.timeline(ctx, session, file)
			if err == nil {
				body, err = hlsVODPlaylist(timeline, start, func(segment transcode.TimelineSegment) string {
					query := url.Values{"api_key": {token}, "PlaySessionId": {session.key.scope.PlaySessionID}}
					return "/emby/" + resource + "/" + url.PathEscape(source.Item.ID) + "/hls1/" + session.id + "/" + strconv.Itoa(segment.Number) + ".ts?" + query.Encode()
				})
			}
		}
		if err != nil {
			s.hlsError(w, r, err)
			return
		}
		// Timeline discovery can take time. Revalidate again before returning a
		// credential-bearing manifest after account, library, or source changes.
		principal := r.Context().Value(principalKey).(identity.Principal)
		verified, _, err := s.authorizeHLS(ctx, principal, session.key.scope, session.key.stamp, session.key.plan)
		if err != nil {
			if permanentHLSError(err) {
				s.hls.retire(session)
			}
			s.hlsError(w, r, err)
			return
		}
		_ = verified.Close()
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		if r.Method != http.MethodHead {
			_, _ = w.Write(body)
		}
	}
}

func (s *Server) hlsSegment(audioOnly bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, done, ok := s.beginHLS(w, r)
		if !ok {
			return
		}
		defer done()
		values, err := hlsValues(r)
		name := r.PathValue("SegmentFile")
		if err != nil || !strings.HasSuffix(strings.ToLower(name), ".ts") {
			s.hlsError(w, r, errHLSRequestInvalid)
			return
		}
		number, err := strconv.ParseUint(name[:len(name)-3], 10, 14)
		if err != nil {
			s.hlsError(w, r, errHLSRequestInvalid)
			return
		}
		values["gobyhlsid"] = r.PathValue("PlaylistId")
		session, file, source, err := s.resolveHLS(ctx, r, values)
		if err != nil {
			s.hlsError(w, r, err)
			return
		}
		if (source.Item.Type == "Audio") != audioOnly {
			_ = file.Close()
			s.hlsError(w, r, library.ErrNotFound)
			return
		}
		work, cancel := context.WithTimeout(ctx, 45*time.Second)
		stopSession := context.AfterFunc(session.ctx, cancel)
		defer func() { stopSession(); cancel() }()
		handle, err := s.hls.segment(work, session, file, int(number))
		if err != nil {
			s.hlsError(w, r, err)
			return
		}
		defer handle.Close()
		principal := r.Context().Value(principalKey).(identity.Principal)
		verified, _, err := s.authorizeHLS(work, principal, session.key.scope, session.key.stamp, session.key.plan)
		if err != nil {
			if permanentHLSError(err) {
				s.hls.retire(session)
			}
			s.hlsError(w, r, err)
			return
		}
		_ = verified.Close()
		info, err := handle.Stat()
		if err != nil {
			s.hlsError(w, r, err)
			return
		}
		digest := sha256.Sum256([]byte(session.id + ":" + name + ":" + strconv.FormatInt(info.Size(), 10) + ":" + strconv.FormatInt(info.ModTime().UnixNano(), 10)))
		w.Header().Set("Content-Type", "video/mp2t")
		w.Header().Set("Cache-Control", "private, no-transform")
		w.Header().Set("ETag", "\""+hex.EncodeToString(digest[:])+"\"")
		w.Header().Set("Access-Control-Expose-Headers", "Accept-Ranges, Content-Length, Content-Range, ETag, Last-Modified")
		controller := http.NewResponseController(w)
		_ = controller.SetWriteDeadline(time.Now().Add(30 * time.Second))
		interrupted := make(chan struct{})
		stop := context.AfterFunc(work, func() { _ = controller.SetWriteDeadline(time.Now()); _ = handle.Close(); close(interrupted) })
		http.ServeContent(w, r, "segment.ts", info.ModTime(), handle)
		if !stop() {
			<-interrupted
		}
		_ = controller.SetWriteDeadline(time.Time{})
	}
}

func (s *Server) stopHLSEncodings(w http.ResponseWriter, r *http.Request) {
	values, err := hlsValues(r)
	if err != nil || values["playsessionid"] == "" {
		s.hlsError(w, r, errHLSRequestInvalid)
		return
	}
	principal := r.Context().Value(principalKey).(identity.Principal)
	if device, supplied := values["deviceid"]; !supplied || device != principal.Client.DeviceID {
		s.hlsError(w, r, library.ErrForbidden)
		return
	}
	// Unknown/foreign keys are inert and idempotent; no ownership is disclosed.
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	canonical, err := s.library.ResolvePlaybackReference(ctx, playbackOwner(principal), values["playsessionid"])
	if err != nil && !errors.Is(err, library.ErrNotFound) && !errors.Is(err, library.ErrForbidden) {
		s.hlsError(w, r, err)
		return
	}
	if err == nil {
		s.hls.cancelMatching(principal.SessionID, canonical)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) hlsError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, context.Canceled):
		if r.Context().Err() != nil {
			return
		}
		apiError(w, r, http.StatusNotFound, "hls_not_found", "The HLS request was cancelled.")
	case errors.Is(err, identity.ErrUnauthorized):
		s.identityError(w, r, err)
	case errors.Is(err, errHLSRequestInvalid), errors.Is(err, library.ErrInvalidInput), errors.Is(err, transcode.ErrInvalidPlan), errors.Is(err, transcode.ErrInvalidTimeline):
		apiError(w, r, http.StatusBadRequest, "invalid_hls_request", "Check HLS parameters and stream identifiers.")
	case errors.Is(err, errHLSRequestUnsupported), errors.Is(err, transcode.ErrUnsupportedTimeline), errors.Is(err, transcode.ErrTimelineLimit):
		apiError(w, r, http.StatusUnsupportedMediaType, "NoCompatibleStream", "This source does not support the requested HLS output.")
	case errors.Is(err, transcode.ErrBusy), errors.Is(err, transcode.ErrQuota):
		w.Header().Set("Retry-After", "2")
		apiError(w, r, http.StatusTooManyRequests, "RateLimitExceeded", "HLS conversion capacity is unavailable.")
	case errors.Is(err, library.ErrForbidden):
		apiError(w, r, http.StatusForbidden, "playback_denied", "The requested playback is not permitted.")
	case errors.Is(err, library.ErrNotFound), errors.Is(err, transcode.ErrJobNotFound), errors.Is(err, transcode.ErrJobCancelled):
		apiError(w, r, http.StatusNotFound, "hls_not_found", "The HLS resource is no longer available.")
	default:
		apiError(w, r, http.StatusServiceUnavailable, "hls_unavailable", "The HLS output could not be prepared.")
	}
}
