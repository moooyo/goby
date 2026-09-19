package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/dynamicsource"
	"github.com/moooyo/goby/internal/subtitle"
)

const (
	dynamicSubtitlePlaylistBytes    = 256 << 10
	dynamicSubtitlePlaylistSegments = 256
	dynamicSubtitleMapBytes         = 64 << 10
	dynamicSubtitlePollBytes        = 16 << 20
	dynamicSubtitleRequestTimeout   = 15 * time.Second
	dynamicSubtitleIdleTimeout      = 15 * time.Second
	dynamicSubtitleClockLimitTicks  = int64(30*24*60*60) * subtitle.TicksPerSecond
)

var (
	errDynamicSubtitleSource     = errors.New("dynamic subtitle source unavailable")
	errDynamicSubtitlePlaylist   = errors.New("invalid dynamic subtitle playlist")
	errDynamicSubtitleContinuity = errors.New("dynamic subtitle playlist continuity changed")
	errDynamicSubtitleWatermark  = errors.New("invalid dynamic subtitle completeness watermark")
)

// dynamicSubtitleSourceUpdate carries source evidence without inventing an
// absolute media clock. A segment-complete event certifies only that its listed
// subtitle bytes were parsed. It is not a media or journal watermark. Callers
// fence every callback by lease generation, resolve MPEGTS maps against an
// explicit measured anchor, and account for the declared OffsetTicks.
type dynamicSubtitleSourceUpdate struct {
	Batch                 subtitle.LiveBatch
	Clock                 string
	OffsetTicks           int64
	HasSegment            bool
	Sequence              int64
	DiscontinuitySequence int64
	DurationTicks         int64
	Discontinuity         bool
	SegmentComplete       bool
	// These are source LOCAL coordinates backed by the operator's explicit
	// timestamp-map contract, not a guessed media epoch. The caller must map
	// both interval endpoints with this update's own Header.TimestampMap.
	IntervalLocalStartTicks *int64
	IntervalDurationTicks   int64
	// WatermarkTicks is an explicit goby-note-v1 declaration in the source's
	// configured clock. It still requires generation and clock validation.
	WatermarkTicks *int64
	PlaylistEnd    bool
	// SourceComplete applies only to a finite document or a fully consumed
	// ENDLIST. EOF on a streaming response is returned as io.EOF instead.
	SourceComplete bool
}

// readDynamicSubtitleSource accepts only a private operator definition already
// resolved by Manager.Subtitle. It owns one HTTP connection at a time and no
// background retry worker. emit must honor ctx if it blocks; returning an error
// immediately closes the response. No URL or credential enters emitted data.
func readDynamicSubtitleSource(ctx context.Context, definition dynamicsource.SubtitleDefinition, emit func(dynamicSubtitleSourceUpdate) error) error {
	if emit == nil || dynamicsource.ValidateSubtitleDefinitions([]dynamicsource.SubtitleDefinition{definition}) != nil {
		return dynamicsource.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.TLSHandshakeTimeout = 5 * time.Second
	transport.ResponseHeaderTimeout = 8 * time.Second
	transport.MaxResponseHeaderBytes = 64 << 10
	transport.MaxConnsPerHost, transport.MaxIdleConnsPerHost = 1, 1
	transport.DisableCompression = true
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	reader := dynamicSubtitleSourceReader{client: client, definition: definition, emit: emit}
	switch definition.Mode {
	case "document":
		data, err := reader.fetch(ctx, definition.URL, subtitle.MaxInputBytes)
		if err != nil {
			return err
		}
		format, err := subtitle.NormalizeFormat(definition.Format)
		if err != nil {
			return err
		}
		header, err := reader.document(ctx, data, format, dynamicSubtitleSourceUpdate{})
		if err != nil {
			return err
		}
		return reader.publish(ctx, dynamicSubtitleSourceUpdate{Batch: subtitle.LiveBatch{Header: header}, SourceComplete: true})
	case "webvtt-stream":
		work, cancel := context.WithCancel(ctx)
		defer cancel()
		response, err := reader.open(work, definition.URL)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		body := &dynamicSubtitleIdleBody{body: response.Body, timeout: dynamicSubtitleIdleTimeout}
		defer body.Close()
		// An endless partial header is not an established caption stream.
		startup := time.AfterFunc(dynamicSubtitleRequestTimeout, cancel)
		defer startup.Stop()
		var watermark int64
		haveWatermark := false
		err = subtitle.ReadLiveWebVTT(body, subtitle.LiveParserOptions{}, func(batch subtitle.LiveBatch) error {
			startup.Stop()
			update := dynamicSubtitleSourceUpdate{Batch: batch}
			ticks, declared, err := dynamicSubtitleWatermark(batch.Note)
			if err != nil {
				return err
			}
			if declared {
				if haveWatermark && ticks < watermark {
					return errDynamicSubtitleWatermark
				}
				if haveWatermark && ticks == watermark {
					return nil
				}
				watermark, haveWatermark = ticks, true
				update.WatermarkTicks = &ticks
			} else if batch.Note != "" {
				return nil
			}
			return reader.publish(work, update)
		})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if work.Err() != nil {
			return errDynamicSubtitleSource
		}
		if err != nil {
			return err
		}
		return io.EOF
	case "webvtt-hls":
		return reader.playlist(ctx)
	}
	return dynamicsource.ErrInvalid
}

type dynamicSubtitleSourceReader struct {
	client     *http.Client
	definition dynamicsource.SubtitleDefinition
	emit       func(dynamicSubtitleSourceUpdate) error
}

func (reader *dynamicSubtitleSourceReader) publish(ctx context.Context, update dynamicSubtitleSourceUpdate) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if update.Batch.Document.Format != "" && reader.definition.Clock == "mpegts" && update.Batch.Header.TimestampMap == nil {
		return subtitle.ErrLiveTimestampMap
	}
	update.Clock, update.OffsetTicks = reader.definition.Clock, reader.definition.OffsetTicks
	return reader.emit(update)
}

func (reader *dynamicSubtitleSourceReader) open(ctx context.Context, target string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, dynamicsource.ErrInvalid
	}
	for name, value := range reader.definition.Headers {
		request.Header.Set(name, value)
	}
	request.Header.Set("Accept-Encoding", "identity")
	response, err := reader.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errDynamicSubtitleSource
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Encoding") != "" && !strings.EqualFold(response.Header.Get("Content-Encoding"), "identity") {
		_ = response.Body.Close()
		return nil, errDynamicSubtitleSource
	}
	return response, nil
}

func (reader *dynamicSubtitleSourceReader) fetch(ctx context.Context, target string, maximum int) ([]byte, error) {
	work, cancel := context.WithTimeout(ctx, dynamicSubtitleRequestTimeout)
	defer cancel()
	response, err := reader.open(work, target)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.ContentLength > int64(maximum) {
		return nil, subtitle.ErrLimitExceeded
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, int64(maximum)+1))
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, errDynamicSubtitleSource
	}
	if len(data) > maximum {
		return nil, subtitle.ErrLimitExceeded
	}
	return data, nil
}

func (reader *dynamicSubtitleSourceReader) document(ctx context.Context, data []byte, format subtitle.Format, update dynamicSubtitleSourceUpdate) (subtitle.LiveHeader, error) {
	// Validate a complete finite response before publishing any part of it.
	document, err := subtitle.Parse(data, format)
	if err != nil {
		return subtitle.LiveHeader{}, err
	}
	canonical, err := subtitle.Render(document, subtitle.Options{Format: subtitle.FormatWebVTT, CopyTimestamps: true})
	if err != nil {
		return subtitle.LiveHeader{}, err
	}
	options := subtitle.LiveParserOptions{MaxHeaderBytes: subtitle.MaxInputBytes, MaxBlockBytes: subtitle.MaxInputBytes, MaxMetadataBytes: subtitle.MaxInputBytes}
	var header subtitle.LiveHeader
	err = subtitle.ReadLiveWebVTT(bytes.NewReader(canonical.Data), options, func(batch subtitle.LiveBatch) error {
		if update.HasSegment && (batch.Header.TimestampMap == nil || batch.Header.TimestampMap.LocalTicks > dynamicSubtitleClockLimitTicks-update.DurationTicks) {
			return subtitle.ErrLiveTimestampMap
		}
		header = subtitle.LiveHeader{Lines: append([]string(nil), batch.Header.Lines...)}
		if batch.Header.TimestampMap != nil {
			mapping := *batch.Header.TimestampMap
			header.TimestampMap = &mapping
		}
		value := update
		value.Batch = batch
		return reader.publish(ctx, value)
	})
	return header, err
}

type dynamicSubtitleIdleBody struct {
	body    io.ReadCloser
	timeout time.Duration
	once    sync.Once
}

func (body *dynamicSubtitleIdleBody) Read(data []byte) (int, error) {
	timer := time.AfterFunc(body.timeout, func() { _ = body.Close() })
	n, err := body.body.Read(data)
	timer.Stop()
	return n, err
}

func (body *dynamicSubtitleIdleBody) Close() error {
	var err error
	body.once.Do(func() { err = body.body.Close() })
	return err
}

type dynamicSubtitleSegment struct {
	URI                   string
	MapURI                string
	Sequence              int64
	DiscontinuitySequence int64
	DurationTicks         int64
	Discontinuity         bool
}

type dynamicSubtitlePlaylist struct {
	Sequence      int64
	TargetSeconds int
	Ended         bool
	Segments      []dynamicSubtitleSegment
}

func (reader *dynamicSubtitleSourceReader) playlist(ctx context.Context) error {
	base, _ := url.Parse(reader.definition.URL)
	var previous map[int64]dynamicSubtitleSegment
	lastSequence, lastDiscontinuity, firstSequence := int64(-1), int64(0), int64(-1)
	for {
		data, err := reader.fetch(ctx, base.String(), dynamicSubtitlePlaylistBytes)
		if err != nil {
			return err
		}
		list, err := parseDynamicSubtitlePlaylist(data)
		if err != nil {
			return err
		}
		if firstSequence >= 0 && list.Sequence < firstSequence {
			return errDynamicSubtitleContinuity
		}
		if lastSequence >= 0 && list.Sequence > lastSequence+1 {
			return errDynamicSubtitleContinuity
		}
		if lastSequence >= 0 && (len(list.Segments) == 0 || list.Segments[len(list.Segments)-1].Sequence < lastSequence) {
			return errDynamicSubtitleContinuity
		}
		current := make(map[int64]dynamicSubtitleSegment, len(list.Segments))
		maps := make(map[string][]byte)
		consumed := len(data)
		for _, segment := range list.Segments {
			current[segment.Sequence] = segment
			if old, exists := previous[segment.Sequence]; exists {
				// A sliding playlist may replace its leading discontinuity tag
				// with DISCONTINUITY-SEQUENCE. The absolute counter stays fixed.
				old.Discontinuity = segment.Discontinuity
				if old != segment {
					return errDynamicSubtitleContinuity
				}
			}
			if segment.Sequence <= lastSequence {
				continue
			}
			if lastSequence >= 0 && (segment.Sequence != lastSequence+1 || segment.DiscontinuitySequence < lastDiscontinuity ||
				segment.DiscontinuitySequence-lastDiscontinuity > 1) {
				return errDynamicSubtitleContinuity
			}
			target, err := dynamicSubtitleRelativeURL(base, segment.URI)
			if err != nil {
				return err
			}
			var header []byte
			if segment.MapURI != "" {
				var exists bool
				header, exists = maps[segment.MapURI]
				if !exists {
					mapping, err := dynamicSubtitleRelativeURL(base, segment.MapURI)
					if err != nil {
						return err
					}
					header, err = reader.fetch(ctx, mapping, dynamicSubtitleMapBytes)
					if err != nil {
						return err
					}
					consumed += len(header)
					if consumed > dynamicSubtitlePollBytes {
						return subtitle.ErrLimitExceeded
					}
					parsed, err := subtitle.Parse(header, subtitle.FormatWebVTT)
					if err != nil || len(parsed.Cues) != 0 {
						return errDynamicSubtitlePlaylist
					}
					maps[segment.MapURI] = header
				}
			}
			payload, err := reader.fetch(ctx, target, min(subtitle.MaxInputBytes, dynamicSubtitlePollBytes-consumed))
			if err != nil {
				return err
			}
			consumed += len(payload)
			// An inherited EXT-X-MAP cannot establish this segment's start.
			// Every segment must carry its own complete WebVTT header and map.
			if !bytes.HasPrefix(bytes.TrimPrefix(payload, []byte{0xef, 0xbb, 0xbf}), []byte("WEBVTT")) {
				return subtitle.ErrLiveTimestampMap
			}
			update := dynamicSubtitleSourceUpdate{HasSegment: true, Sequence: segment.Sequence, DiscontinuitySequence: segment.DiscontinuitySequence,
				DurationTicks: segment.DurationTicks, Discontinuity: segment.Discontinuity || lastSequence >= 0 && segment.DiscontinuitySequence != lastDiscontinuity, PlaylistEnd: list.Ended}
			segmentHeader, err := reader.document(ctx, payload, subtitle.FormatWebVTT, update)
			if err != nil {
				return err
			}
			if segmentHeader.TimestampMap == nil || segmentHeader.TimestampMap.LocalTicks > dynamicSubtitleClockLimitTicks-segment.DurationTicks {
				return subtitle.ErrLiveTimestampMap
			}
			start := segmentHeader.TimestampMap.LocalTicks
			update.Batch.Header = segmentHeader
			update.SegmentComplete = true
			update.IntervalLocalStartTicks, update.IntervalDurationTicks = &start, segment.DurationTicks
			if err := reader.publish(ctx, update); err != nil {
				return err
			}
			lastSequence, lastDiscontinuity = segment.Sequence, segment.DiscontinuitySequence
		}
		previous, firstSequence = current, list.Sequence
		if list.Ended {
			return reader.publish(ctx, dynamicSubtitleSourceUpdate{PlaylistEnd: true, SourceComplete: true})
		}
		interval := time.Duration(list.TargetSeconds) * time.Second / 2
		interval = max(time.Second, min(10*time.Second, interval))
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func dynamicSubtitleRelativeURL(base *url.URL, reference string) (string, error) {
	if len(reference) == 0 || len(reference) > 8192 || !utf8.ValidString(reference) || strings.ContainsAny(reference, "\r\n\x00\\") {
		return "", errDynamicSubtitlePlaylist
	}
	child, err := url.Parse(reference)
	if err != nil || child.Scheme != "" || child.Host != "" || child.User != nil || child.Opaque != "" || child.Fragment != "" || child.Path == "" ||
		strings.Contains(child.Path, "%") || strings.ContainsAny(child.Path, "\\\r\n\x00") {
		return "", errDynamicSubtitlePlaylist
	}
	query, err := url.QueryUnescape(child.RawQuery)
	if err != nil || !utf8.ValidString(child.Path) || !utf8.ValidString(query) || strings.IndexFunc(child.Path+query, func(character rune) bool { return character < 0x20 || character == 0x7f }) >= 0 {
		return "", errDynamicSubtitlePlaylist
	}
	escaped := strings.ToLower(child.EscapedPath())
	if strings.Contains(escaped, "%2f") || strings.Contains(escaped, "%5c") {
		return "", errDynamicSubtitlePlaylist
	}
	for _, part := range strings.Split(child.Path, "/") {
		if part == "." || part == ".." {
			return "", errDynamicSubtitlePlaylist
		}
	}
	target := base.ResolveReference(child)
	if target.Scheme != base.Scheme || target.Host != base.Host || target.User != nil {
		return "", errDynamicSubtitlePlaylist
	}
	return target.String(), nil
}

func parseDynamicSubtitlePlaylist(data []byte) (dynamicSubtitlePlaylist, error) {
	invalid := func() (dynamicSubtitlePlaylist, error) { return dynamicSubtitlePlaylist{}, errDynamicSubtitlePlaylist }
	if len(data) == 0 || len(data) > dynamicSubtitlePlaylistBytes || !utf8.Valid(data) || bytes.ContainsRune(data, 0) {
		return invalid()
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if lines[0] != "#EXTM3U" {
		return invalid()
	}
	list := dynamicSubtitlePlaylist{}
	seen := make(map[string]bool)
	var duration, discontinuity int64
	var boundary bool
	mapURI := ""
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		if len(line) > 8192 || strings.ContainsAny(line, "\r\t") || list.Ended {
			return invalid()
		}
		if !strings.HasPrefix(line, "#") {
			if duration <= 0 || len(list.Segments) >= dynamicSubtitlePlaylistSegments || list.Sequence > math.MaxInt64-int64(len(list.Segments))-1 {
				return invalid()
			}
			list.Segments = append(list.Segments, dynamicSubtitleSegment{URI: line, MapURI: mapURI, Sequence: list.Sequence + int64(len(list.Segments)),
				DiscontinuitySequence: discontinuity, DurationTicks: duration, Discontinuity: boundary})
			duration, boundary = 0, false
			continue
		}
		name, value, hasValue := strings.Cut(line, ":")
		switch name {
		case "#EXTINF":
			if !hasValue || duration != 0 {
				return invalid()
			}
			var ok bool
			seconds, _, comma := strings.Cut(value, ",")
			duration, ok = dynamicSubtitleDuration(seconds)
			if !comma || !ok {
				return invalid()
			}
		case "#EXT-X-DISCONTINUITY":
			if hasValue || boundary || duration != 0 || discontinuity == math.MaxInt64 {
				return invalid()
			}
			discontinuity++
			boundary = true
		case "#EXT-X-MAP":
			if !hasValue || duration != 0 || !strings.HasPrefix(value, "URI=\"") || !strings.HasSuffix(value, "\"") {
				return invalid()
			}
			mapURI = strings.TrimSuffix(strings.TrimPrefix(value, "URI=\""), "\"")
			if mapURI == "" || strings.Contains(mapURI, "\"") {
				return invalid()
			}
		case "#EXT-X-ENDLIST":
			if hasValue || duration != 0 || boundary {
				return invalid()
			}
			list.Ended = true
		case "#EXT-X-TARGETDURATION", "#EXT-X-MEDIA-SEQUENCE", "#EXT-X-DISCONTINUITY-SEQUENCE", "#EXT-X-VERSION":
			if !hasValue || seen[name] || len(list.Segments) != 0 || duration != 0 || boundary {
				return invalid()
			}
			seen[name] = true
			if value == "" || len(value) > 20 {
				return invalid()
			}
			for _, character := range value {
				if character < '0' || character > '9' {
					return invalid()
				}
			}
			number, err := strconv.ParseInt(value, 10, 64)
			if err != nil || number < 0 {
				return invalid()
			}
			switch name {
			case "#EXT-X-TARGETDURATION":
				if number < 1 || number > 3600 {
					return invalid()
				}
				list.TargetSeconds = int(number)
			case "#EXT-X-MEDIA-SEQUENCE":
				list.Sequence = number
			case "#EXT-X-DISCONTINUITY-SEQUENCE":
				discontinuity = number
			case "#EXT-X-VERSION":
				if number < 1 || number > 10 {
					return invalid()
				}
			}
		case "#EXT-X-PLAYLIST-TYPE":
			if !hasValue || seen[name] || len(list.Segments) != 0 || value != "VOD" && value != "EVENT" {
				return invalid()
			}
			seen[name] = true
		case "#EXT-X-INDEPENDENT-SEGMENTS":
			if hasValue || seen[name] {
				return invalid()
			}
			seen[name] = true
		case "#EXT-X-PROGRAM-DATE-TIME":
			if !hasValue {
				return invalid()
			}
			if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
				return invalid()
			}
		default:
			if strings.HasPrefix(name, "#EXT") {
				return invalid()
			}
		}
	}
	if !seen["#EXT-X-TARGETDURATION"] || duration != 0 || boundary {
		return invalid()
	}
	for _, segment := range list.Segments {
		if (segment.DurationTicks+subtitle.TicksPerSecond/2)/subtitle.TicksPerSecond > int64(list.TargetSeconds) {
			return invalid()
		}
	}
	return list, nil
}

func dynamicSubtitleDuration(value string) (int64, bool) {
	seconds, fraction, dot := strings.Cut(value, ".")
	if seconds == "" || len(seconds) > 4 || len(fraction) > 7 || dot && fraction == "" {
		return 0, false
	}
	for _, part := range []string{seconds, fraction} {
		for _, character := range part {
			if character < '0' || character > '9' {
				return 0, false
			}
		}
	}
	whole, err := strconv.ParseInt(seconds, 10, 64)
	if err != nil || whole > 3600 {
		return 0, false
	}
	if dot {
		fraction += strings.Repeat("0", 7-len(fraction))
	} else {
		fraction = "0"
	}
	partial, err := strconv.ParseInt(fraction, 10, 64)
	if err != nil {
		return 0, false
	}
	ticks := whole*subtitle.TicksPerSecond + partial
	return ticks, ticks > 0 && ticks <= 3600*subtitle.TicksPerSecond
}

func dynamicSubtitleWatermark(note string) (int64, bool, error) {
	const marker = "NOTE GOBY-WATERMARK"
	if !strings.HasPrefix(note, marker) {
		return 0, false, nil
	}
	if !strings.HasPrefix(note, marker+" ") || strings.ContainsAny(note, "\r\n") {
		return 0, false, errDynamicSubtitleWatermark
	}
	clock := strings.TrimPrefix(note, marker+" ")
	parts := strings.Split(clock, ":")
	if len(parts) != 3 || len(parts[0]) < 2 || len(parts[0]) > 3 || len(parts[1]) != 2 || len(parts[2]) != 6 || parts[2][2] != '.' {
		return 0, false, errDynamicSubtitleWatermark
	}
	for _, value := range []string{parts[0], parts[1], parts[2][:2], parts[2][3:]} {
		for _, character := range value {
			if character < '0' || character > '9' {
				return 0, false, errDynamicSubtitleWatermark
			}
		}
	}
	document, err := subtitle.Parse([]byte("WEBVTT\n\n"+clock+" --> "+clock+"\nwatermark\n"), subtitle.FormatWebVTT)
	if err != nil || len(document.Cues) != 1 || document.Cues[0].StartTicks > dynamicSubtitleClockLimitTicks {
		return 0, false, errDynamicSubtitleWatermark
	}
	return document.Cues[0].StartTicks, true, nil
}
