package server

import (
	"errors"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/moooyo/goby/internal/primaryio"
)

type originalPrimaryHeaderPlan struct {
	header http.Header
	status int
}

func (p *originalPrimaryHeaderPlan) Header() http.Header { return p.header }
func (p *originalPrimaryHeaderPlan) WriteHeader(status int) {
	if p.status == 0 {
		p.status = status
	}
}
func (p *originalPrimaryHeaderPlan) Write(data []byte) (int, error) {
	if p.status == 0 {
		p.status = http.StatusOK
	}
	return len(data), nil
}

type originalPrimaryRange struct{ start, length int64 }

// A single-range HEAD probe delegates validators and If-Range to ServeContent
// without invoking its multipart producer. The original range syntax retains
// the pinned standard library's suffix, trimming and skipped-overlap behavior.
func originalPrimaryRanges(header string, size int64) ([]originalPrimaryRange, error) {
	if !strings.HasPrefix(header, "bytes=") || size <= 0 {
		return nil, io.ErrUnexpectedEOF
	}
	var ranges []originalPrimaryRange
	for _, part := range strings.Split(strings.TrimPrefix(header, "bytes="), ",") {
		part = textproto.TrimString(part)
		if part == "" {
			continue
		}
		first, last, found := strings.Cut(part, "-")
		if !found {
			return nil, io.ErrUnexpectedEOF
		}
		first, last = textproto.TrimString(first), textproto.TrimString(last)
		if first == "" {
			length, err := strconv.ParseInt(last, 10, 64)
			if err != nil || length < 0 || last == "" || strings.HasPrefix(last, "-") {
				return nil, io.ErrUnexpectedEOF
			}
			length = min(length, size)
			ranges = append(ranges, originalPrimaryRange{size - length, length})
			continue
		}
		start, err := strconv.ParseInt(first, 10, 64)
		if err != nil || start < 0 {
			return nil, io.ErrUnexpectedEOF
		}
		if start >= size {
			continue
		}
		end := size - 1
		if last != "" {
			end, err = strconv.ParseInt(last, 10, 64)
			if err != nil || end < start {
				return nil, io.ErrUnexpectedEOF
			}
			end = min(end, size-1)
		}
		ranges = append(ranges, originalPrimaryRange{start, end - start + 1})
	}
	return ranges, nil
}

func originalPrimaryRangeHeaders(region originalPrimaryRange, contentType string, size int64) textproto.MIMEHeader {
	return textproto.MIMEHeader{
		"Content-Range": {fmt.Sprintf("bytes %d-%d/%d", region.start, region.start+region.length-1, size)},
		"Content-Type":  {contentType},
	}
}

type originalPrimarySizeCounter struct{ bytes int64 }

func (c *originalPrimarySizeCounter) Write(data []byte) (int, error) {
	if int64(len(data)) > math.MaxInt64-c.bytes {
		return 0, io.ErrUnexpectedEOF
	}
	c.bytes += int64(len(data))
	return len(data), nil
}

func originalPrimaryMultipartSize(boundary, contentType string, size int64, ranges []originalPrimaryRange) (int64, error) {
	counter := &originalPrimarySizeCounter{}
	writer := multipart.NewWriter(counter)
	if err := writer.SetBoundary(boundary); err != nil {
		return 0, err
	}
	var dataBytes int64
	for _, region := range ranges {
		if _, err := writer.CreatePart(originalPrimaryRangeHeaders(region, contentType, size)); err != nil {
			return 0, err
		}
		if region.length > math.MaxInt64-dataBytes {
			return 0, io.ErrUnexpectedEOF
		}
		dataBytes += region.length
	}
	if err := writer.Close(); err != nil {
		return 0, err
	}
	if counter.bytes > math.MaxInt64-dataBytes {
		return 0, io.ErrUnexpectedEOF
	}
	return counter.bytes + dataBytes, nil
}

func copyOriginalPrimaryRange(writer io.Writer, reader io.Reader, remaining int64, buffer []byte) error {
	for remaining > 0 {
		length := len(buffer)
		if remaining < int64(length) {
			length = int(remaining)
		}
		n, readErr := reader.Read(buffer[:length])
		if n < 0 || n > length {
			return primaryio.ErrReadResult
		}
		if n > 0 {
			written, writeErr := writer.Write(buffer[:n])
			if writeErr != nil {
				return writeErr
			}
			if written != n {
				return io.ErrShortWrite
			}
			remaining -= int64(n)
		}
		if readErr != nil {
			if remaining == 0 && errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
		if n == 0 {
			return io.ErrNoProgress
		}
	}
	return nil
}

// Genuine multipart GET bodies are copied synchronously with one reusable
// 32 KiB buffer. This avoids ServeContent's unjoined pipe producer while keeping
// the standard conditional/If-Range plan. Other responses use ServeContent.
func serveOriginalPrimaryContent(w http.ResponseWriter, r *http.Request, name string, modified time.Time, contentType string, size int64, content *primaryio.ReadSeeker) error {
	if r.Method != http.MethodGet && r.Method != http.MethodHead || !strings.Contains(r.Header.Get("Range"), ",") {
		http.ServeContent(w, r, name, modified, content)
		return nil
	}
	head := r.Clone(r.Context())
	head.Method = http.MethodHead
	// Multi-range HEAD also starts an unjoined producer in the pinned standard
	// library. A single-range probe exercises the same preconditions without it.
	head.Header.Set("Range", "bytes=0-0")
	plan := &originalPrimaryHeaderPlan{header: w.Header().Clone()}
	http.ServeContent(plan, head, name, modified, content)
	if plan.status != http.StatusPartialContent {
		http.ServeContent(w, r, name, modified, content)
		return nil
	}
	ranges, err := originalPrimaryRanges(r.Header.Get("Range"), size)
	if err != nil || len(ranges) < 2 {
		http.ServeContent(w, r, name, modified, content)
		return nil
	}
	var rangeBytes int64
	for _, region := range ranges {
		if region.length > size-rangeBytes {
			full := r.Clone(r.Context())
			full.Header.Del("Range")
			http.ServeContent(w, full, name, modified, content)
			return nil
		}
		rangeBytes += region.length
	}
	writer := multipart.NewWriter(w)
	length, err := originalPrimaryMultipartSize(writer.Boundary(), contentType, size, ranges)
	if err != nil {
		return err
	}
	plan.header.Del("Content-Range")
	plan.header.Set("Content-Type", "multipart/byteranges; boundary="+writer.Boundary())
	plan.header.Set("Content-Length", strconv.FormatInt(length, 10))
	for key := range w.Header() {
		delete(w.Header(), key)
	}
	for key, values := range plan.header {
		w.Header()[key] = append([]string(nil), values...)
	}
	w.WriteHeader(plan.status)
	if r.Method == http.MethodHead {
		return nil
	}
	buffer := make([]byte, mediaWriteChunk)
	for _, region := range ranges {
		if err := r.Context().Err(); err != nil {
			return err
		}
		part, err := writer.CreatePart(originalPrimaryRangeHeaders(region, contentType, size))
		if err != nil {
			return err
		}
		if _, err := content.Seek(region.start, io.SeekStart); err != nil {
			return err
		}
		if err := copyOriginalPrimaryRange(part, content, region.length, buffer); err != nil {
			return err
		}
	}
	return writer.Close()
}
