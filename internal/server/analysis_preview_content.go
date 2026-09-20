package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// A BIF response accepts exactly one byte range. Multi-range, malformed and
// unsatisfiable requests return 416; an unmatched If-Range selects full content.
// Dates are deliberately not validators: source identities and immutable
// derivative digests are more precise than second-resolution modification time.
func (s *Server) serveAnalysisPreviewContent(w http.ResponseWriter, r *http.Request, content io.ReadSeeker, size int64, contentType, etag string, ranges bool) {
	if r.Context().Err() != nil {
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, no-cache, no-transform")
	if ranges {
		w.Header().Set("Accept-Ranges", "bytes")
	}
	w.Header().Set("Access-Control-Expose-Headers", "Accept-Ranges, Content-Length, Content-Range, ETag")
	if values, present := r.Header["If-Match"]; present && !analysisPreviewStrongMatch(strings.Join(values, ","), etag) {
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusPreconditionFailed)
		return
	}
	if matchesImageETag(strings.Join(r.Header.Values("If-None-Match"), ","), etag) {
		w.Header().Del("Content-Length")
		w.WriteHeader(http.StatusNotModified)
		return
	}
	start, length, status := int64(0), size, http.StatusOK
	if rangeValues := r.Header.Values("Range"); ranges && len(rangeValues) != 0 {
		ifRange := r.Header.Values("If-Range")
		useRange := len(ifRange) == 0 || len(ifRange) == 1 && ifRange[0] == etag
		if useRange {
			var valid bool
			if len(rangeValues) == 1 {
				start, length, valid = analysisPreviewSingleRange(rangeValues[0], size)
			}
			if !valid {
				w.Header().Set("Content-Range", "bytes */"+strconv.FormatInt(size, 10))
				w.Header().Set("Content-Length", "0")
				w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
				return
			}
			status = http.StatusPartialContent
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, start+length-1, size))
		}
	}
	if _, err := content.Seek(start, io.SeekStart); err != nil {
		w.Header().Del("ETag")
		w.Header().Del("Content-Range")
		s.analysisPreviewError(w, r, err)
		return
	}
	writer, err := newIdleResponseWriter(w, r.Context(), mediaWriteIdle)
	if err != nil {
		panic(http.ErrAbortHandler)
	}
	defer writer.finish()
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))
	writer.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	if _, err := io.CopyN(writer, analysisPreviewContextReader{context: r.Context(), reader: content}, length); err != nil || writer.err != nil || r.Context().Err() != nil {
		panic(http.ErrAbortHandler)
	}
}

type analysisPreviewContextReader struct {
	context context.Context
	reader  io.Reader
}

func (reader analysisPreviewContextReader) Read(buffer []byte) (int, error) {
	if err := reader.context.Err(); err != nil {
		return 0, err
	}
	n, err := reader.reader.Read(buffer)
	if cancelled := reader.context.Err(); cancelled != nil {
		return n, cancelled
	}
	return n, err
}

func analysisPreviewStrongMatch(header, etag string) bool {
	for _, value := range strings.Split(header, ",") {
		if value = strings.TrimSpace(value); value == "*" || value == etag {
			return true
		}
	}
	return false
}

func analysisPreviewSingleRange(value string, size int64) (int64, int64, bool) {
	rest, ok := strings.CutPrefix(value, "bytes=")
	if !ok || size <= 0 || strings.ContainsAny(rest, ", \t\r\n") {
		return 0, 0, false
	}
	first, last, ok := strings.Cut(rest, "-")
	if !ok {
		return 0, 0, false
	}
	number := func(raw string) (int64, bool) {
		if raw == "" {
			return 0, false
		}
		for _, character := range raw {
			if character < '0' || character > '9' {
				return 0, false
			}
		}
		value, err := strconv.ParseInt(raw, 10, 64)
		return value, err == nil
	}
	if first == "" {
		length, ok := number(last)
		if !ok || length == 0 {
			return 0, 0, false
		}
		length = min(length, size)
		return size - length, length, true
	}
	start, ok := number(first)
	if !ok || start >= size {
		return 0, 0, false
	}
	end := size - 1
	if last != "" {
		end, ok = number(last)
		if !ok || end < start {
			return 0, 0, false
		}
		end = min(end, size-1)
	}
	return start, end - start + 1, true
}
