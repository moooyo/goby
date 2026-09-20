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
	"github.com/moooyo/goby/internal/library"
	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/subtitle"
)

func (s *Server) registerSubtitleRoutes(mux *http.ServeMux) {
	for _, resource := range []string{"Videos", "Items"} {
		base := "/emby/" + resource + "/{Id}/{MediaSourceId}/Subtitles/{Index}/"
		mux.HandleFunc("GET "+base+"{SubtitleFileName}", s.requireEmby(s.subtitleStream))
		mux.HandleFunc("GET "+base+"{StartPositionTicks}/{SubtitleFileName}", s.requireEmby(s.subtitleStream))
		mux.HandleFunc("GET /emby/"+resource+"/{Id}/{MediaSourceId}/Attachments/{Index}/Stream", s.requireEmby(s.subtitleAttachment))
		management := "/emby/" + resource + "/{Id}/Subtitles/{Index}"
		mux.HandleFunc("DELETE "+management, s.requireEmby(s.deleteSubtitle))
		mux.HandleFunc("POST "+management+"/Delete", s.requireEmby(s.deleteSubtitle))
	}
}

func readSubtitleOptions(r *http.Request) (int, subtitle.Options, error) {
	options := subtitle.Options{PreserveSource: true}
	name := strings.ToLower(r.PathValue("SubtitleFileName"))
	if !strings.HasPrefix(name, "stream.") {
		return 0, options, subtitle.ErrUnsupportedFormat
	}
	format, err := subtitle.NormalizeFormat(strings.TrimPrefix(name, "stream."))
	if err != nil {
		return 0, options, subtitle.ErrUnsupportedFormat
	}
	options.Format = format
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
	if raw, supplied := values["subtitleoffsetticks"]; supplied {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < -subtitle.MaxOffsetTicks || value > subtitle.MaxOffsetTicks {
			return 0, options, subtitle.ErrInvalidRange
		}
		options.OffsetTicks = value
	}
	return int(index), options, nil
}

// readSubtitleContentFor shares current source authorization between sidecar
// delivery and embedded extraction. Every cache validator still passes through
// this path; extracted files are never cached across permission changes.
func (s *Server) readSubtitleContentFor(ctx context.Context, subject library.Subject, itemID, sourceID string, index int, format subtitle.Format) (library.SubtitleContent, error) {
	if index < 0 || sourceID == "" {
		return library.SubtitleContent{}, library.ErrInvalidInput
	}
	file, source, err := s.library.OpenMediaFor(ctx, subject, itemID, sourceID)
	if err != nil {
		return library.SubtitleContent{}, err
	}
	defer file.Close()
	if source.Item.Media == nil {
		return library.SubtitleContent{}, library.ErrNotFound
	}
	for _, stream := range source.Item.Media.Streams {
		if stream.Index != index {
			continue
		}
		if stream.CodecType != "subtitle" {
			return library.SubtitleContent{}, library.ErrNotFound
		}
		if media.SubtitleExtractFormat(stream.Codec) == "" {
			return library.SubtitleContent{}, subtitle.ErrUnsupportedFormat
		}
		native := media.SubtitleExtractFormat(stream.Codec)
		// Preserve ASS script/style data until Render chooses a representation.
		// FFmpeg handles codec conversion for other embedded text formats.
		if native != "ass" && format != "" {
			native = string(format)
		}
		data, err := media.ExtractSubtitle(ctx, s.cfg.FFmpegPath, file, stream, native)
		if err != nil {
			return library.SubtitleContent{}, err
		}
		return library.SubtitleContent{Data: data, Info: library.Subtitle{Index: index, Codec: native,
			Language: stream.Language, Title: stream.Title, IsDefault: stream.IsDefault, IsForced: stream.IsForced,
			Size: int64(len(data)), ModifiedAt: source.ModifiedAt}, ModifiedAt: source.ModifiedAt}, nil
	}
	return s.library.ReadSubtitleFor(ctx, subject, itemID, sourceID, index)
}

func (s *Server) subtitleStream(w http.ResponseWriter, r *http.Request) {
	index, options, err := readSubtitleOptions(r)
	if err != nil {
		s.subtitleError(w, r, err)
		return
	}
	values, err := streamValues(r)
	if err != nil {
		s.subtitleError(w, r, subtitle.ErrInvalidRange)
		return
	}
	ownedTag, ownedRequested := values["gobysubtitletag"]
	if ownedRequested {
		if digest, err := hex.DecodeString(ownedTag); err != nil || len(digest) != sha256.Size || strings.ToLower(ownedTag) != ownedTag {
			s.subtitleError(w, r, subtitle.ErrInvalidRange)
			return
		}
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
	var content library.SubtitleContent
	if ownedRequested {
		// A replaced source may introduce an embedded stream at an old external
		// index. An issued owned-track URL must never select that new stream.
		content, err = s.library.ReadSubtitleFor(ctx, librarySubject(principal, principal.User.ID), r.PathValue("Id"), r.PathValue("MediaSourceId"), index)
		if err == nil && (!content.Info.Owned || content.Info.Tag != ownedTag) {
			err = library.ErrNotFound
		}
	} else {
		content, err = s.readSubtitleContentFor(ctx, librarySubject(principal, principal.User.ID), r.PathValue("Id"), r.PathValue("MediaSourceId"), index, options.Format)
	}
	if err != nil {
		if errors.Is(err, subtitle.ErrUnsupportedFormat) || errors.Is(err, media.ErrSubtitleExtraction) {
			s.subtitleError(w, r, err)
		} else {
			s.playbackError(w, r, err)
		}
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
	writer, err := newIdleResponseWriter(w, r.Context(), mediaWriteIdle)
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	defer writer.finish()
	digest := sha256.Sum256(result.Data)
	w.Header().Set("ETag", strconv.Quote(hex.EncodeToString(digest[:])))
	w.Header().Set("Content-Type", result.ContentType)
	w.Header().Set("Cache-Control", "private, no-cache, no-transform")
	w.Header().Set("Access-Control-Expose-Headers", "Accept-Ranges, Content-Length, Content-Range, ETag, Last-Modified")
	// The indexed source and its current permissions were checked before either
	// a cached/conditional response or the rendered representation is served.
	http.ServeContent(writer, r, "subtitle."+string(options.Format), content.ModifiedAt, bytes.NewReader(result.Data))
}

func (s *Server) subtitleAttachment(w http.ResponseWriter, r *http.Request) {
	index, err := strconv.Atoi(r.PathValue("Index"))
	if err != nil || index < 0 || index > 4095 || r.PathValue("MediaSourceId") == "" {
		s.subtitleError(w, r, subtitle.ErrInvalidRange)
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
	file, source, err := s.library.OpenMediaFor(ctx, librarySubject(principal, principal.User.ID), r.PathValue("Id"), r.PathValue("MediaSourceId"))
	if err != nil {
		s.playbackError(w, r, err)
		return
	}
	defer file.Close()
	if source.Item.Media == nil {
		s.playbackError(w, r, library.ErrNotFound)
		return
	}
	for _, stream := range source.Item.Media.Streams {
		if stream.Index != index || !media.FontAttachment(stream) {
			continue
		}
		data, err := media.ExtractFontAttachment(ctx, s.cfg.FFmpegPath, file, stream)
		if err != nil {
			s.subtitleError(w, r, err)
			return
		}
		digest := sha256.Sum256(data)
		w.Header().Set("ETag", strconv.Quote(hex.EncodeToString(digest[:])))
		w.Header().Set("Content-Type", "font/ttf")
		if string(data[:4]) == "OTTO" {
			w.Header().Set("Content-Type", "font/otf")
		}
		w.Header().Set("Content-Disposition", `attachment; filename="font-`+strconv.Itoa(index)+`.ttf"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "private, no-cache, no-transform")
		writer, err := newIdleResponseWriter(w, r.Context(), mediaWriteIdle)
		if err != nil {
			panic(http.ErrAbortHandler)
		}
		defer writer.finish()
		http.ServeContent(writer, r, "font-"+strconv.Itoa(index)+".ttf", source.ModifiedAt, bytes.NewReader(data))
		return
	}
	s.playbackError(w, r, library.ErrNotFound)
}

func (s *Server) subtitleError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, subtitle.ErrUnsupportedFormat):
		apiError(w, r, http.StatusUnsupportedMediaType, "subtitle_format_unavailable", "The requested subtitle format is not supported.")
	case errors.Is(err, subtitle.ErrInvalidRange):
		apiError(w, r, http.StatusBadRequest, "invalid_subtitle_request", "Check subtitle indexes, tick positions, and timestamp options.")
	case errors.Is(err, subtitle.ErrLimitExceeded), errors.Is(err, media.ErrOutputLimit):
		apiError(w, r, http.StatusRequestEntityTooLarge, "subtitle_limit", "The subtitle representation exceeds its resource limit.")
	default:
		apiError(w, r, http.StatusServiceUnavailable, "subtitle_unavailable", "The indexed subtitle cannot currently be rendered.")
	}
}
