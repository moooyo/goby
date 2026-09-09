package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/subtitle"
)

func (s *Server) registerSubtitleRoutes(mux *http.ServeMux) {
	for _, resource := range []string{"Videos", "Items"} {
		base := "/emby/" + resource + "/{Id}/{MediaSourceId}/Subtitles/{Index}/"
		mux.HandleFunc("GET "+base+"{SubtitleFileName}", s.requireEmby(s.subtitleStream))
		mux.HandleFunc("GET "+base+"{StartPositionTicks}/{SubtitleFileName}", s.requireEmby(s.subtitleStream))
	}
}

func readSubtitleOptions(r *http.Request) (int, subtitle.Options, error) {
	options := subtitle.Options{PreserveSource: true}
	name := strings.ToLower(r.PathValue("SubtitleFileName"))
	if !strings.HasPrefix(name, "stream.") {
		return 0, options, subtitle.ErrUnsupportedFormat
	}
	switch strings.TrimPrefix(name, "stream.") {
	case "srt":
		options.Format = subtitle.FormatSRT
	case "vtt":
		options.Format = subtitle.FormatWebVTT
	default:
		return 0, options, subtitle.ErrUnsupportedFormat
	}
	index, err := strconv.ParseInt(r.PathValue("Index"), 10, 32)
	if err != nil || index < 0 {
		return 0, options, subtitle.ErrInvalidRange
	}
	values, err := streamValues(r)
	if err != nil {
		return 0, options, subtitle.ErrInvalidRange
	}
	parseTicks := func(raw string) (int64, error) {
		ticks, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || ticks < 0 {
			return 0, subtitle.ErrInvalidRange
		}
		return ticks, nil
	}
	pathStart := r.PathValue("StartPositionTicks")
	if pathStart != "" {
		options.StartTicks, err = parseTicks(pathStart)
		if err != nil {
			return 0, options, err
		}
	}
	if raw, supplied := values["startpositionticks"]; supplied {
		value, err := parseTicks(raw)
		if err != nil {
			return 0, options, subtitle.ErrInvalidRange
		}
		// The reference applies an explicit query value after the path value.
		options.StartTicks = value
	}
	if raw, supplied := values["endpositionticks"]; supplied {
		value, err := parseTicks(raw)
		if err != nil {
			return 0, options, err
		}
		options.EndTicks = &value
	}
	if raw, supplied := values["copytimestamps"]; supplied {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return 0, options, subtitle.ErrInvalidRange
		}
		options.CopyTimestamps = value
	}
	return int(index), options, nil
}

func (s *Server) subtitleStream(w http.ResponseWriter, r *http.Request) {
	index, options, err := readSubtitleOptions(r)
	if err != nil {
		s.subtitleError(w, r, err)
		return
	}
	select {
	case s.subtitleSlots <- struct{}{}:
		defer func() { <-s.subtitleSlots }()
	default:
		w.Header().Set("Retry-After", "2")
		apiError(w, r, http.StatusTooManyRequests, "subtitle_limit", "The server has reached its active subtitle request limit.")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	principal := r.Context().Value(principalKey).(identity.Principal)
	content, err := s.library.ReadSubtitleFor(ctx, librarySubject(principal, principal.User.ID), r.PathValue("Id"), r.PathValue("MediaSourceId"), index)
	if err != nil {
		s.playbackError(w, r, err)
		return
	}
	document, err := subtitle.Parse(content.Data, subtitle.Format(content.Info.Codec))
	if err != nil {
		s.subtitleError(w, r, err)
		return
	}
	result, err := subtitle.Render(document, options)
	if err != nil {
		s.subtitleError(w, r, err)
		return
	}
	if ctx.Err() != nil {
		s.playbackError(w, r, ctx.Err())
		return
	}
	digest := sha256.Sum256(result.Data)
	w.Header().Set("ETag", strconv.Quote(hex.EncodeToString(digest[:])))
	w.Header().Set("Content-Type", result.ContentType)
	w.Header().Set("Cache-Control", "private, no-cache, no-transform")
	w.Header().Set("Access-Control-Expose-Headers", "Accept-Ranges, Content-Length, Content-Range, ETag, Last-Modified")
	// The indexed source and its current permissions were checked before either
	// a cached/conditional response or the rendered representation is served.
	http.ServeContent(w, r, "subtitle."+string(options.Format), content.ModifiedAt, bytes.NewReader(result.Data))
}

func (s *Server) subtitleError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, subtitle.ErrUnsupportedFormat):
		apiError(w, r, http.StatusUnsupportedMediaType, "subtitle_format_unavailable", "The requested subtitle format is not supported.")
	case errors.Is(err, subtitle.ErrInvalidRange):
		apiError(w, r, http.StatusBadRequest, "invalid_subtitle_request", "Check subtitle indexes, tick positions, and timestamp options.")
	case errors.Is(err, subtitle.ErrLimitExceeded):
		apiError(w, r, http.StatusRequestEntityTooLarge, "subtitle_limit", "The subtitle representation exceeds its resource limit.")
	default:
		apiError(w, r, http.StatusServiceUnavailable, "subtitle_unavailable", "The indexed subtitle cannot currently be rendered.")
	}
}
