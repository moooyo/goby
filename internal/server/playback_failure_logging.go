package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/moooyo/goby/internal/diagnostics"
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

// Keep the useful branch and failure class without recording source names,
// credential URLs, process arguments, or raw errors from disk or FFmpeg.
func (s *Server) logPlaybackFailure(ctx context.Context, stage string, err error) {
	if s.log == nil || err == nil || errors.Is(err, context.Canceled) ||
		errors.Is(err, library.ErrForbidden) || errors.Is(err, library.ErrNotFound) || errors.Is(err, transcode.ErrJobNotFound) || errors.Is(err, transcode.ErrJobCancelled) {
		return
	}
	class := playbackFailureClass(err)
	attrs := []slog.Attr{slog.String("playback_stage", stage), slog.String("error_class", class)}
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		attrs = append(attrs, slog.String("request_id", id))
	}
	var unavailable *hlsPausedProducerError
	if errors.As(err, &unavailable) && unavailable.jobID != "" {
		attrs = append(attrs, slog.String("job_id", unavailable.jobID))
		if unavailable.reason != "" {
			attrs = append(attrs, slog.String("job_failure", unavailable.reason))
		}
	}
	s.log.LogAttrs(context.Background(), slog.LevelWarn, "playback operation failed", attrs...)
}

func playbackFailureClass(err error) string {
	class := diagnostics.ErrorClass(err)
	if class == "unclassified" {
		switch {
		case errors.Is(err, errHLSPausedProducerUnavailable):
			class = "paused_producer_unavailable"
		case errors.Is(err, errHLSManifestLimit):
			class = "manifest_limit"
		case errors.Is(err, transcode.ErrInvalidPlaylist), errors.Is(err, errInvalidHLSManifest):
			class = "invalid_playlist"
		case errors.Is(err, transcode.ErrOutputUnavailable):
			class = "output_unavailable"
		case errors.Is(err, transcode.ErrJobFailed):
			class = "producer_failed"
		case errors.Is(err, transcode.ErrQuota):
			class = "producer_quota"
		case errors.Is(err, library.ErrSourceChanged):
			class = "source_changed"
		case errors.Is(err, media.ErrOutputLimit):
			class = "output_limit"
		case errors.Is(err, media.ErrSubtitleExtraction):
			class = "subtitle_extraction"
		case errors.Is(err, library.ErrBusy):
			class = "source_busy"
		case errors.Is(err, library.ErrUnavailable):
			class = "source_unavailable"
		case errors.Is(err, transcode.ErrInvalidInput):
			class = "invalid_input"
		case errors.Is(err, transcode.ErrInvalidOptions), errors.Is(err, transcode.ErrInvalidScope):
			class = "invalid_admission"
		case errors.Is(err, transcode.ErrManagerClosed):
			class = "manager_closed"
		case errors.Is(err, transcode.ErrPersistence):
			class = "persistence"
		}
	}
	return class
}

func captureHLSSegmentFailure(ctx context.Context, stage string, err error, jobID string) {
	if err != nil && diagnostics.CapturesPlaybackFailure(ctx) {
		diagnostics.CapturePlaybackFailure(ctx, diagnostics.PlaybackFailure{Stage: stage, ErrorClass: playbackFailureClass(err), JobID: jobID})
	}
}

// Emit only for a response that uses a 503 branch. A successful retry within
// the server, a cancellation, or a rejected/foreign registration is not logged.
func (s *Server) logHLSUnavailable(r *http.Request, code string, observed diagnostics.PlaybackFailure) {
	if s.log == nil {
		return
	}
	id, _ := r.Context().Value(requestIDKey).(string)
	s.log.LogAttrs(context.Background(), slog.LevelWarn, "playback operation failed",
		slog.String("request_id", id), slog.String("playback_stage", observed.Stage),
		slog.String("error_class", observed.ErrorClass), slog.String("job_id", observed.JobID),
		slog.String("hls_error_code", code), slog.Int("status", http.StatusServiceUnavailable))
}
