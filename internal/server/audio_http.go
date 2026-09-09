package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

func (s *Server) audioStream(w http.ResponseWriter, r *http.Request) {
	name := strings.ToLower(r.PathValue("StreamFileName"))
	universal := name == "universal" || strings.HasPrefix(name, "universal.")
	var suffix string
	original := false
	if universal {
		if name != "universal" {
			suffix = strings.TrimPrefix(name, "universal.")
			if suffix == "" || len(suffix) > 32 {
				s.audioError(w, r, errAudioRequestInvalid)
				return
			}
		}
	} else {
		var valid bool
		suffix, original, valid = streamAlias(name)
		if !valid {
			apiError(w, r, http.StatusNotFound, "not_found", "The requested audio route is not available.")
			return
		}
	}
	values, err := hlsValues(r)
	if err != nil {
		s.audioError(w, r, errAudioRequestInvalid)
		return
	}
	principal := r.Context().Value(principalKey).(identity.Principal)
	if !principal.IsApplicationKey() && (values["userid"] != "" && values["userid"] != principal.User.ID ||
		values["deviceid"] != "" && values["deviceid"] != principal.Client.DeviceID) {
		s.audioError(w, r, library.ErrForbidden)
		return
	}
	if original {
		if value, exists := values["static"]; exists && !strings.EqualFold(value, "true") && value != "1" && value != "t" && value != "T" {
			s.audioError(w, r, errAudioRequestInvalid)
			return
		}
		values["static"] = "true"
	}
	select {
	case s.streamSlots <- struct{}{}:
		defer func() { <-s.streamSlots }()
	default:
		w.Header().Set("Retry-After", "2")
		apiError(w, r, http.StatusTooManyRequests, "stream_limit", "The active media stream limit has been reached.")
		return
	}
	prepare, cancelPrepare := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancelPrepare()
	file, source, err := s.library.OpenMediaFor(prepare, librarySubject(principal, principal.User.ID), r.PathValue("Id"), values["mediasourceid"])
	if err != nil {
		s.audioError(w, r, err)
		return
	}
	defer func() {
		if file != nil {
			_ = file.Close()
		}
	}()
	if source.Item.Type != "Audio" {
		s.audioError(w, r, library.ErrNotFound)
		return
	}
	input := playback.Source{ItemID: source.Item.ID, MediaSourceID: source.SourceID,
		Path: source.Item.Path, ItemType: source.Item.Type, Info: playbackMediaInfo(source.Item)}
	decision, err := audioRequestDecision(input, values, suffix, universal, hlsPrincipalLimits(s.cfg.Transcoding, principal))
	if err != nil {
		s.audioError(w, r, err)
		return
	}
	if !decision.Original && (exactAudioCoverage(input.Info, decision.AudioStreamIndex) == nil || s.hls == nil) {
		s.audioError(w, r, errAudioRequestUnsupported)
		return
	}
	var play library.PlaySession
	// Legacy original-file reads retain their independent access contract.
	// Universal URLs and conversions also bind client playback references.
	if universal || !decision.Original {
		if reference := values["playsessionid"]; reference != "" {
			play, err = s.library.PrepareCorrelatedPlayback(prepare, playbackOwner(principal), source.Item.ID, source.SourceID, reference)
		} else {
			play, err = s.library.PreparePlayback(prepare, playbackOwner(principal), source.Item.ID, source.SourceID, "")
		}
		if err != nil {
			s.audioError(w, r, err)
			return
		}
	}
	if decision.Original {
		if universal {
			fresh, err := s.identity.RevalidateSession(prepare, principal)
			if err != nil {
				s.audioError(w, r, err)
				return
			}
			verified, current, err := s.library.OpenMediaFor(prepare, librarySubject(fresh, fresh.User.ID), source.Item.ID, source.SourceID)
			if err != nil {
				s.audioError(w, r, err)
				return
			}
			_ = verified.Close()
			if current.ETag != source.ETag {
				s.audioError(w, r, library.ErrSourceChanged)
				return
			}
		}
		s.serveOriginalMedia(w, r, file, source)
		return
	}
	var conversion playback.ConversionDecision
	if decision.HLS != nil {
		conversion = *decision.HLS
	} else if decision.Progressive != nil {
		conversion = playback.ConversionDecision{Plan: decision.Progressive.Plan, OutputSource: decision.Progressive.OutputSource,
			Method: decision.Progressive.Method}
	} else {
		s.audioError(w, r, errAudioRequestUnsupported)
		return
	}
	session, err := s.hls.register(principal, source, play.ID, conversion, decision.StartTicks)
	if err != nil {
		s.audioError(w, r, err)
		return
	}
	if decision.HLS != nil {
		token, _, _ := parseEmbyCredentials(r)
		target, err := url.Parse(hlsSessionURL(session, "Audio", "master.m3u8", token, decision.StartTicks))
		if err != nil {
			s.audioError(w, r, err)
			return
		}
		forward := r.Clone(r.Context())
		forward.URL = target
		s.hlsPlaylist(true, true)(w, forward)
		return
	}
	owned := file
	file = nil
	s.serveProgressiveMedia(w, r, session, owned)
}

// Conversion must not turn an estimated duration or a delayed/shorter track
// into an exact clipping boundary. Original-file delivery needs no such proof.
func exactAudioCoverage(info media.Info, index int) *media.AudioTiming {
	if !info.AudioDurationExact || info.DurationTicks <= 0 {
		return nil
	}
	for _, stream := range info.Streams {
		if stream.Index != index || stream.CodecType != "audio" || stream.IsExternal || stream.AudioTiming == nil {
			continue
		}
		timing := stream.AudioTiming
		if timing.Exact && timing.StartTicks == 0 && timing.EndTicks == info.DurationTicks && timing.SampleCount > 0 && timing.PacketCount > 0 {
			return timing
		}
	}
	return nil
}

func progressiveAudioMIME(container string) string {
	switch container {
	case "mp3":
		return "audio/mpeg"
	case "aac":
		return "audio/aac"
	case "flac":
		return "audio/flac"
	case "ogg":
		return "audio/ogg"
	case "wav":
		return "audio/wav"
	case "m4a":
		return "audio/mp4"
	default:
		return "application/octet-stream"
	}
}

// serveProgressiveMedia owns input. A header-only request validates the plan
// and permissions but starts no encoder and invents no output Content-Length.
func (s *Server) serveProgressiveMedia(w http.ResponseWriter, r *http.Request, session *hlsSession, input *os.File) {
	respondError := s.audioError
	if session.key.plan.Container == "mp4" {
		respondError = s.videoError
	}
	if !s.hls.enter() {
		_ = input.Close()
		respondError(w, r, transcode.ErrManagerClosed)
		return
	}
	defer s.hls.requests.Done()
	lifetime, cancelLifetime := context.WithTimeout(r.Context(), 4*time.Hour)
	defer cancelLifetime()
	work, cancel := context.WithCancelCause(lifetime)
	defer cancel(context.Canceled)
	stopSession := context.AfterFunc(session.ctx, func() { cancel(transcode.ErrJobCancelled) })
	defer stopSession()
	principal := r.Context().Value(principalKey).(identity.Principal)
	if r.Method == http.MethodHead {
		_ = input.Close()
		check, stopCheck := context.WithTimeout(work, 10*time.Second)
		verified, _, err := s.authorizeHLS(check, principal, session.key.scope, session.key.stamp, session.key.plan)
		stopCheck()
		if err != nil {
			if permanentHLSError(err) {
				s.hls.retire(session)
			}
			respondError(w, r, err)
			return
		}
		_ = verified.Close()
		setProgressiveMediaHeaders(w, session.key.plan.Container)
		w.WriteHeader(http.StatusOK)
		return
	}
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Now().Add(time.Minute))
	var transportMu sync.Mutex
	transportAborted := false
	transportStopped := make(chan struct{})
	stopTransport := context.AfterFunc(work, func() {
		transportMu.Lock()
		transportAborted = true
		_ = controller.SetWriteDeadline(time.Now())
		transportMu.Unlock()
		close(transportStopped)
	})
	defer func() {
		if !stopTransport() {
			<-transportStopped
		}
		_ = controller.SetWriteDeadline(time.Time{})
	}()
	startupDone := make(chan struct{})
	startup := time.AfterFunc(45*time.Second, func() {
		cancel(context.DeadlineExceeded)
		close(startupDone)
	})
	reader, release, err := s.hls.progressive(work, session, input)
	if !startup.Stop() {
		<-startupDone
	}
	if err != nil {
		if cause := context.Cause(work); cause != nil {
			err = cause
		}
		respondError(w, r, err)
		return
	}
	defer release()
	defer reader.Close()
	check, stopCheck := context.WithTimeout(work, 10*time.Second)
	verified, _, err := s.authorizeHLS(check, principal, session.key.scope, session.key.stamp, session.key.plan)
	stopCheck()
	if err != nil {
		if permanentHLSError(err) {
			s.hls.retire(session)
		}
		respondError(w, r, err)
		return
	}
	_ = verified.Close()
	setProgressiveMediaHeaders(w, session.key.plan.Container)
	w.WriteHeader(http.StatusOK)
	buffer := make([]byte, 32*1024)
	for {
		count, readErr := reader.Read(buffer)
		if count > 0 {
			transportMu.Lock()
			if transportAborted || work.Err() != nil {
				transportMu.Unlock()
				panic(http.ErrAbortHandler)
			}
			_ = controller.SetWriteDeadline(time.Now().Add(30 * time.Second))
			transportMu.Unlock()
			if written, err := w.Write(buffer[:count]); err != nil || written != count {
				panic(http.ErrAbortHandler)
			}
			if err := controller.Flush(); err != nil {
				panic(http.ErrAbortHandler)
			}
			session.mu.Lock()
			if !session.closed {
				session.accessed = time.Now()
			}
			session.mu.Unlock()
		}
		if errors.Is(readErr, io.EOF) {
			return
		}
		if readErr != nil {
			// A normal chunk terminator would falsely present partial media as
			// a completed stream. Let net/http abort HTTP/1 or reset HTTP/2.
			panic(http.ErrAbortHandler)
		}
	}
}

func setProgressiveMediaHeaders(w http.ResponseWriter, container string) {
	contentType := progressiveAudioMIME(container)
	if container == "mp4" {
		contentType = "video/mp4"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-cache, no-store, no-transform, must-revalidate")
	w.Header().Set("Accept-Ranges", "none")
	w.Header().Del("Content-Length")
	w.Header().Del("Content-Range")
	w.Header().Del("ETag")
}

func (s *Server) audioError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, context.Canceled):
		if r.Context().Err() == nil {
			apiError(w, r, http.StatusNotFound, "audio_cancelled", "The audio output was cancelled.")
		}
	case errors.Is(err, identity.ErrUnauthorized):
		s.identityError(w, r, err)
	case errors.Is(err, errAudioRequestInvalid), errors.Is(err, playback.ErrInvalidRequest), errors.Is(err, library.ErrInvalidInput):
		apiError(w, r, http.StatusBadRequest, "invalid_audio_request", "Check audio parameters and stream identifiers.")
	case errors.Is(err, errAudioRequestUnsupported), errors.Is(err, transcode.ErrInvalidPlan), errors.Is(err, transcode.ErrUnsupportedTimeline):
		apiError(w, r, http.StatusUnsupportedMediaType, "NoCompatibleStream", "This source does not support the requested audio output.")
	case errors.Is(err, transcode.ErrBusy), errors.Is(err, transcode.ErrQuota), errors.Is(err, library.ErrBusy):
		w.Header().Set("Retry-After", "2")
		apiError(w, r, http.StatusTooManyRequests, "RateLimitExceeded", "Audio playback capacity is unavailable.")
	case errors.Is(err, library.ErrForbidden):
		apiError(w, r, http.StatusForbidden, "playback_denied", "The requested playback is not permitted.")
	case errors.Is(err, library.ErrNotFound), errors.Is(err, transcode.ErrJobNotFound), errors.Is(err, transcode.ErrJobCancelled):
		apiError(w, r, http.StatusNotFound, "audio_not_found", "The audio resource is no longer available.")
	default:
		apiError(w, r, http.StatusServiceUnavailable, "audio_unavailable", "The audio output could not be prepared.")
	}
}
