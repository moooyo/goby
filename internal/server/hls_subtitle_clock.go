package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/subtitle"
	"github.com/moooyo/goby/internal/transcode"
)

func (s *Server) serveGeneratedHLSSubtitle(w http.ResponseWriter, r *http.Request, session *hlsSession, input *os.File) bool {
	select {
	case s.subtitleSlots <- struct{}{}:
		defer func() { <-s.subtitleSlots }()
	default:
		w.Header().Set("Retry-After", "2")
		apiError(w, r, http.StatusTooManyRequests, "subtitle_limit", "The server has reached its active subtitle request limit.")
		return false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	delta, err := s.hls.subtitleClock(ctx, session, input)
	if err != nil {
		s.hlsError(w, r, err)
		return false
	}
	principal := r.Context().Value(principalKey).(identity.Principal)
	content, err := s.readSubtitleContentFor(ctx, librarySubject(principal, principal.User.ID), session.key.scope.ItemID, session.key.scope.SourceID, session.key.plan.Subtitle.StreamIndex, subtitle.FormatWebVTT)
	if err != nil {
		s.subtitleError(w, r, err)
		return false
	}
	document, err := subtitle.Parse(content.Data, subtitle.Format(content.Info.Codec))
	if err != nil {
		s.subtitleError(w, r, err)
		return false
	}
	result, err := subtitle.RenderHLS(document, session.key.plan.Subtitle.OffsetTicks, delta)
	if err != nil {
		s.subtitleError(w, r, err)
		return false
	}
	if !s.revalidateGeneratedHLS(ctx, w, r, session) {
		return false
	}
	if err := ctx.Err(); err != nil {
		s.hlsError(w, r, err)
		return false
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
	return writer.err == nil && (writer.status == http.StatusOK || writer.status == http.StatusPartialContent || writer.status == http.StatusNotModified)
}

type hlsClockJobs interface {
	HLSClock(context.Context, transcode.Scope, string, int) (transcode.HLSMuxClock, error)
}

// subtitleClock measures the container's translation of the producer's source
// clock. Pairing the very same first reference packet before and after muxing
// avoids guesses about decoder reorder delay, AAC priming, and MP4 timescales.
func (h *hlsRuntime) subtitleClock(ctx context.Context, session *hlsSession, input *os.File) (int64, error) {
	clocks, ok := h.manager.(hlsClockJobs)
	if !ok || session.key.plan.Subtitle.Mode != "hls" {
		return 0, transcode.ErrOutputUnavailable
	}
	first, err := h.generatedArtifact(ctx, session, input, transcode.HLSPlaylistName(0, session.key.plan.HLS.RenditionCount))
	if err != nil {
		return 0, err
	}
	defer first.Close()
	jobID := first.EncodingID()
	if jobID == "" {
		return 0, transcode.ErrOutputUnavailable
	}
	for {
		session.mu.Lock()
		if session.closed {
			session.mu.Unlock()
			return 0, transcode.ErrJobCancelled
		}
		if session.subtitleClockJob == jobID {
			delta := session.subtitleClockTicks
			session.mu.Unlock()
			return delta, nil
		}
		if pending := session.subtitleClockBusy; pending != nil {
			session.mu.Unlock()
			select {
			case <-ctx.Done():
				return 0, ctx.Err()
			case <-pending:
				continue
			}
		}
		pending := make(chan struct{})
		session.subtitleClockBusy = pending
		session.mu.Unlock()
		delta, err := h.measureSubtitleClock(ctx, session, clocks, jobID, first)
		session.mu.Lock()
		if err == nil && !session.closed {
			session.subtitleClockJob, session.subtitleClockTicks = jobID, delta
		}
		session.subtitleClockBusy = nil
		close(pending)
		session.mu.Unlock()
		return delta, err
	}
}

func (h *hlsRuntime) measureSubtitleClock(ctx context.Context, session *hlsSession, clocks hlsClockJobs, jobID string, first *transcode.ReadHandle) (int64, error) {
	var sharedDelta int64
	for index := 0; index < max(1, session.key.plan.HLS.RenditionCount); index++ {
		playlist := first
		if index != 0 {
			var err error
			playlist, err = h.manager.Open(ctx, session.key.scope, jobID, transcode.HLSPlaylistName(index, session.key.plan.HLS.RenditionCount))
			if err != nil {
				return 0, err
			}
		}
		data, err := io.ReadAll(io.LimitReader(playlist, transcode.MaxPlaylistBytes+1))
		if index != 0 {
			_ = playlist.Close()
		}
		if err != nil {
			return 0, err
		}
		list, err := transcode.ParseMediaPlaylist(data)
		if err != nil || list.Sequence != 0 || len(list.Segments) == 0 || list.Segments[0].Number != 0 {
			return 0, transcode.ErrInvalidTimeline
		}
		before, err := clocks.HLSClock(ctx, session.key.scope, jobID, index)
		if err != nil {
			return 0, err
		}
		preTicks, err := before.Ticks()
		if err != nil {
			return 0, err
		}
		segment, err := h.manager.Open(ctx, session.key.scope, jobID, list.Segments[0].Name)
		if err != nil {
			return 0, err
		}
		var init *transcode.ReadHandle
		if list.InitName != "" {
			init, err = h.manager.Open(ctx, session.key.scope, jobID, list.InitName)
			if err != nil {
				_ = segment.Close()
				return 0, err
			}
		}
		var initFile *os.File
		if init != nil {
			initFile = init.File
		}
		select {
		case h.probes <- struct{}{}:
			var postTicks int64
			postTicks, err = transcode.MeasureHLSMuxClock(ctx, h.server.cfg.FFprobePath, initFile, segment.File, session.key.plan.VideoStreamIndex >= 0)
			<-h.probes
			if err == nil {
				delta := postTicks - preTicks
				if delta < -24*60*60*media.TicksPerSecond || delta > 24*60*60*media.TicksPerSecond {
					err = transcode.ErrInvalidTimeline
				} else if index == 0 {
					sharedDelta = delta
				} else if delta-sharedDelta > 112 || sharedDelta-delta > 112 {
					err = transcode.ErrInvalidTimeline
				}
			}
		case <-ctx.Done():
			err = ctx.Err()
		}
		_ = segment.Close()
		if init != nil {
			_ = init.Close()
		}
		if err != nil {
			return 0, err
		}
	}
	return sharedDelta, nil
}
