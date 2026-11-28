package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/subtitle"
	"github.com/moooyo/goby/internal/transcode"
)

// Native HLS cannot reliably retry an application-level preparation response.
// Preserve its previous extraction wait without changing the HLS clock or
// tying the shared worker's lifetime to the client.
func hlsSubtitleExtractionWait(header http.Header) time.Duration {
	for _, value := range header.Values("Prefer") {
		for _, preference := range strings.Split(value, ",") {
			token, _, _ := strings.Cut(preference, ";")
			if strings.EqualFold(strings.TrimSpace(token), "respond-async") {
				return subtitleExtractionFastWait
			}
		}
	}
	return subtitleExtractionNativeWait
}

func (s *Server) serveGeneratedHLSSubtitle(w http.ResponseWriter, r *http.Request, session *hlsSession, input *os.File, slot int, sequence int64, view playback.HLSSubtitleView, producerIDs ...string) (delivered, subtitleOnlyFailure bool) {
	select {
	case s.subtitleSlots <- struct{}{}:
		defer func() { <-s.subtitleSlots }()
	default:
		w.Header().Set("Retry-After", "2")
		apiError(w, r, http.StatusTooManyRequests, "subtitle_limit", "The server has reached its active subtitle request limit.")
		return false, true
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	_, windows, delta, err := s.hls.subtitleMediaWindow(ctx, session, input, producerIDs...)
	if err != nil {
		s.logPlaybackFailure(ctx, "hls_subtitle_manifest", err)
		s.hlsError(w, r, err)
		return false, false
	}
	principal := r.Context().Value(principalKey).(identity.Principal)
	track, ok := transcode.HLSSubtitleTrackAt(session.key.plan, slot)
	if !ok {
		s.hlsError(w, r, transcode.ErrJobNotFound)
		return false, false
	}
	window, exists := windows[sequence]
	if sequence >= 0 && !exists {
		s.hlsError(w, r, transcode.ErrJobNotFound)
		return false, false
	}
	w.Header().Add("Vary", "Prefer")
	content, err := s.readSubtitleContentFor(ctx, principal, session.key.scope.ItemID, session.key.scope.SourceID, track.StreamIndex, subtitle.FormatWebVTT, session, true, hlsSubtitleExtractionWait(r.Header))
	if err != nil {
		switch {
		case permanentHLSError(err) || errors.Is(err, context.Canceled):
			s.hlsError(w, r, err)
		default:
			s.subtitleError(w, r, err)
		}
		// Body preparation cannot fail A/V startup. An actual authorization or
		// source failure still follows the normal HLS error path.
		return false, !permanentHLSError(err)
	}
	document, err := subtitle.Parse(content.Data, subtitle.Format(content.Info.Codec))
	if err != nil {
		s.logPlaybackFailure(ctx, "subtitle_render", err)
		s.subtitleError(w, r, err)
		return false, true
	}
	var result subtitle.Result
	if sequence < 0 {
		result, err = subtitle.RenderHLS(document, view.OffsetTicks, delta)
	} else {
		result, err = subtitle.RenderHLSWindow(document, max(0, window.Start), max(0, window.End), view.OffsetTicks, delta)
	}
	if err != nil {
		s.logPlaybackFailure(ctx, "subtitle_render", err)
		s.subtitleError(w, r, err)
		return false, true
	}
	if !s.revalidateGeneratedHLS(ctx, w, r, session) {
		return false, false
	}
	if err := ctx.Err(); err != nil {
		s.hlsError(w, r, err)
		return false, false
	}
	writer, err := newIdleResponseWriter(w, ctx, mediaWriteIdle)
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	defer writer.finish()
	digest := sha256.Sum256(result.Data)
	w.Header().Set("ETag", strconv.Quote(hex.EncodeToString(digest[:])))
	w.Header().Set("Content-Type", result.ContentType)
	w.Header().Set("Cache-Control", "private, no-cache, no-transform")
	w.Header().Set("Access-Control-Expose-Headers", "Accept-Ranges, Content-Length, Content-Range, ETag, Last-Modified")
	http.ServeContent(writer, r.WithContext(ctx), "subtitle.vtt", content.ModifiedAt, bytes.NewReader(result.Data))
	if ctx.Err() != nil {
		panic(http.ErrAbortHandler)
	}
	return writer.err == nil && (writer.status == http.StatusOK || writer.status == http.StatusPartialContent || writer.status == http.StatusNotModified), false
}

type hlsClockJobs interface {
	HLSClock(context.Context, transcode.Scope, string, int) (transcode.HLSMuxClock, error)
}

// subtitleClock measures the container's translation of the producer's source
// clock. Pairing the very same first reference packet before and after muxing
// avoids guesses about decoder reorder delay, AAC priming, and MP4 timescales.
func (h *hlsRuntime) subtitleClock(ctx context.Context, session *hlsSession, input *os.File, producerIDs ...string) (int64, error) {
	evidence, err := h.producerSubtitleClock(ctx, session, input, producerIDs...)
	return evidence.delta, err
}
