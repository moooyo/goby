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

// Every subtitle response (including a cache hit) starts with a fresh source
// open. A bound HLS track keeps the extraction policy captured by its plan.
func (s *Server) readSubtitleContentFor(ctx context.Context, principal identity.Principal, itemID, sourceID string, index int, format subtitle.Format, session *hlsSession, startExtraction bool, wait time.Duration) (library.SubtitleContent, error) {
	if index < 0 || sourceID == "" {
		return library.SubtitleContent{}, library.ErrInvalidInput
	}
	subject := librarySubject(principal, principal.User.ID)
	file, source, err := s.library.RevalidateMediaSourceFor(ctx, subject, itemID, sourceID, false)
	if err != nil {
		s.logPlaybackFailure(ctx, "subtitle_input", err)
		return library.SubtitleContent{}, err
	}
	stream, found := embeddedSubtitleStream(source, index)
	if !found {
		if err := file.Close(); err != nil {
			return library.SubtitleContent{}, err
		}
		content, err := s.library.ReadSubtitleFor(ctx, subject, itemID, sourceID, index)
		if err != nil {
			s.logPlaybackFailure(ctx, "subtitle_input", err)
		}
		return content, err
	}
	if stream.CodecType != "subtitle" {
		_ = file.Close()
		return library.SubtitleContent{}, library.ErrNotFound
	}
	if session != nil && (session.ctx.Err() != nil || session.key.stamp != source.ETag) {
		_ = file.Close()
		return library.SubtitleContent{}, library.ErrSourceChanged
	}
	native := media.SubtitleExtractFormat(stream.Codec)
	if native == "" || session == nil && !s.settingsForContext(ctx).Transcoding.EnableSubtitleExtraction {
		_ = file.Close()
		return library.SubtitleContent{}, subtitle.ErrUnsupportedFormat
	}
	// Preserve ASS script/style data until Render chooses a representation.
	// Other embedded codecs keep their existing FFmpeg output representation.
	if native != "ass" && format != "" {
		native = string(format)
	}
	command := s.cfg.FFmpegPath
	toolPath, tool := subtitleExtractionTool(command)
	var data []byte
	if tool != nil {
		if err := file.Close(); err != nil {
			return library.SubtitleContent{}, err
		}
		version := source.ReadVersion()
		if version == (library.MediaSourceReadVersion{}) {
			return library.SubtitleContent{}, library.ErrUnavailable
		}
		requestID, _ := ctx.Value(requestIDKey).(string)
		key := subtitleExtractionKey{item: itemID, source: sourceID, version: version, index: index, codec: stream.Codec, format: native, tool: toolPath}
		work := subtitleExtractionWork{key: key, tool: tool, command: command, actor: subtitleExtractionActor(principal), bound: session != nil, requestID: requestID}
		var load func(context.Context) ([]byte, error)
		if startExtraction {
			load = func(lifetime context.Context) ([]byte, error) { return s.extractBackgroundSubtitle(lifetime, work) }
		}
		data, err = s.subtitleExtractions.read(ctx, key, tool, wait, load)
		if err == nil && session == nil {
			// Waiting for shared work cannot borrow the admitting client's
			// authority. Verify the standalone caller again before delivery.
			var current *os.File
			current, _, _, err = s.openSubtitleExtractionSource(ctx, work, false)
			if err == nil {
				err = current.Close()
			}
		}
	} else if !startExtraction {
		err = errors.Join(errSubtitlePreparing, file.Close())
	} else {
		data, err = s.extractUncacheableSubtitle(ctx, command, file, source, stream, native)
	}
	if err != nil {
		return library.SubtitleContent{}, err
	}
	return library.SubtitleContent{Data: data, Info: library.Subtitle{Index: index, Codec: native,
		Language: stream.Language, Title: stream.Title, IsDefault: stream.IsDefault, IsForced: stream.IsForced,
		Size: int64(len(data)), ModifiedAt: source.ModifiedAt}, ModifiedAt: source.ModifiedAt}, nil
}

// Keep aliases with unwitnessed invocation dependencies out of the shared cache.
func (s *Server) extractUncacheableSubtitle(ctx context.Context, command string, file *os.File, source library.MediaFile, stream media.Stream, format string) (data []byte, resultErr error) {
	stage := "subtitle_input"
	defer func() { s.logPlaybackFailure(ctx, stage, resultErr) }()
	read, err := s.library.PrepareMediaSourceIO(ctx, source)
	defer func() { resultErr = errors.Join(resultErr, closePrimaryMediaSource(file, read)) }()
	if err != nil {
		return nil, err
	}
	err = read.Run(ctx, func(work context.Context) error {
		stage = "subtitle_extraction"
		var err error
		data, err = media.ExtractSubtitle(work, command, file, stream, format)
		return err
	})
	return data, err
}

// Negotiation validates sidecars but never waits for embedded subtitle bytes.
// A real subtitle GET schedules the selected track if its cache is cold.
func (s *Server) validatePlaybackExternalSubtitle(ctx context.Context, principal identity.Principal, source library.MediaFile, index int) error {
	if _, embedded := embeddedSubtitleStream(source, index); embedded {
		return nil
	}
	_, err := s.library.ReadSubtitleFor(ctx, librarySubject(principal, principal.User.ID), source.Item.ID, source.SourceID, index)
	return err
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
		content, err = s.readSubtitleContentFor(ctx, principal, r.PathValue("Id"), r.PathValue("MediaSourceId"), index, options.Format, nil, r.Method == http.MethodGet, subtitleExtractionFastWait)
	}
	if err != nil {
		if subtitleExtractionPending(err) || errors.Is(err, subtitle.ErrUnsupportedFormat) || errors.Is(err, media.ErrSubtitleExtraction) || errors.Is(err, subtitle.ErrLimitExceeded) {
			s.subtitleError(w, r, err)
		} else {
			s.playbackError(w, r, err)
		}
		return
	}
	document, err := subtitle.Parse(content.Data, subtitle.Format(content.Info.Codec))
	if err != nil {
		s.logPlaybackFailure(ctx, "subtitle_render", err)
		s.subtitleError(w, r, err)
		return
	}
	result, err := subtitle.Render(document, options)
	if err != nil {
		s.logPlaybackFailure(ctx, "subtitle_render", err)
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
	if !s.requestSettings(r).Transcoding.EnableOnTheFlyAttachmentExtraction {
		apiError(w, r, http.StatusServiceUnavailable, "subtitle_attachment_extraction_disabled", "On-the-fly attachment extraction is disabled.")
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
	read, err := s.library.PrepareMediaSourceIO(ctx, source)
	if err != nil {
		_ = file.Close()
		s.subtitleError(w, r, err)
		return
	}
	defer closePrimaryMediaSource(file, read)
	ctx = read.Context(ctx)
	if source.Item.Media == nil {
		s.playbackError(w, r, library.ErrNotFound)
		return
	}
	for _, stream := range source.Item.Media.Streams {
		if stream.Index != index || !media.FontAttachment(stream) {
			continue
		}
		var data []byte
		err := media.RunSourceReadPhase(ctx, func(work context.Context) error {
			var err error
			data, err = media.ExtractFontAttachment(work, s.cfg.FFmpegPath, file, stream)
			return err
		})
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

func subtitleExtractionPending(err error) bool {
	return errors.Is(err, errSubtitlePreparing) || errors.Is(err, errSubtitleExtractionBusy)
}

func (s *Server) subtitleError(w http.ResponseWriter, r *http.Request, err error) {
	if subtitleExtractionPending(err) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Retry-After", "2")
		w.Header().Set(subtitlePreparingHeader, "1")
		w.Header().Set("Access-Control-Expose-Headers", "Retry-After, "+subtitlePreparingHeader)
	}
	switch {
	case errors.Is(err, errSubtitlePreparing):
		apiError(w, r, http.StatusServiceUnavailable, "subtitle_preparing", "The embedded subtitle is being prepared. Retry shortly.")
	case errors.Is(err, errSubtitleExtractionBusy):
		apiError(w, r, http.StatusTooManyRequests, "subtitle_extraction_busy", "The embedded subtitle preparation queue is full. Retry shortly.")
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
