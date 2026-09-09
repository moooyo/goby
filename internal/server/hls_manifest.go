package server

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

const (
	maxHLSManifestBytes    = 2 * 1024 * 1024
	maxHLSManifestURLBytes = 8192
)

var (
	errInvalidHLSManifest = errors.New("invalid HLS manifest")
	errHLSManifestLimit   = errors.New("HLS manifest exceeds its resource limit")
)

// hlsMasterPlaylist describes one authorized media playlist. URLs are ASCII URI
// references, so non-ASCII path characters must be percent-encoded. No codec or
// segment-independence claim can be inferred from these arguments.
func hlsMasterPlaylist(mediaURL string, bandwidth int64, width, height int) ([]byte, error) {
	if !validHLSManifestURL(mediaURL) || bandwidth <= 0 || width < 0 || height < 0 || (width == 0) != (height == 0) {
		return nil, errInvalidHLSManifest
	}
	var result strings.Builder
	result.WriteString("#EXTM3U\n#EXT-X-VERSION:3\n")
	fmt.Fprintf(&result, "#EXT-X-STREAM-INF:BANDWIDTH=%d", bandwidth)
	if width > 0 {
		fmt.Fprintf(&result, ",RESOLUTION=%dx%d", width, height)
	}
	result.WriteByte('\n')
	result.WriteString(mediaURL)
	result.WriteByte('\n')
	return []byte(result.String()), nil
}

// hlsVODPlaylist advertises the complete source timeline, even when playback
// starts later. The caller authorizes access and supplies token-bearing child
// URLs; this function neither authenticates nor performs I/O.
func hlsVODPlaylist(timeline transcode.Timeline, startTicks int64, childURL func(transcode.TimelineSegment) string) ([]byte, error) {
	if childURL == nil || len(timeline.Segments) == 0 || timeline.TargetDuration <= 0 || startTicks < 0 {
		return nil, errInvalidHLSManifest
	}
	if len(timeline.Segments) > transcode.MaxTimelineSegments {
		return nil, errHLSManifestLimit
	}
	// A callback may retain the caller's timeline. Snapshot the bounded segment
	// list so callback-side changes cannot invalidate an already checked span.
	segments := append([]transcode.TimelineSegment(nil), timeline.Segments...)
	var total int64
	for number, segment := range segments {
		if segment.Number != number || segment.StartTicks != total || segment.DurationTicks <= 0 || segment.DurationTicks > math.MaxInt64-total {
			return nil, errInvalidHLSManifest
		}
		// RFC 8216 section 4.3.3.1 compares durations rounded to the nearest
		// integer with TARGETDURATION. Avoid addition that could overflow.
		roundedSeconds := segment.DurationTicks / media.TicksPerSecond
		if segment.DurationTicks%media.TicksPerSecond >= media.TicksPerSecond/2 {
			roundedSeconds++
		}
		if roundedSeconds > int64(timeline.TargetDuration) {
			return nil, errInvalidHLSManifest
		}
		total += segment.DurationTicks
	}
	if startTicks >= total {
		return nil, errInvalidHLSManifest
	}
	var result strings.Builder
	fmt.Fprintf(&result, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n", timeline.TargetDuration)
	if startTicks > 0 {
		// RFC 8216 section 4.3.5.2 defines PRECISE=YES as a request to omit
		// rendered samples before the offset, not an independent-GOP claim.
		fmt.Fprintf(&result, "#EXT-X-START:TIME-OFFSET=%d.%07d,PRECISE=YES\n", startTicks/media.TicksPerSecond, startTicks%media.TicksPerSecond)
	}
	const endList = "#EXT-X-ENDLIST\n"
	for _, segment := range segments {
		child := childURL(segment)
		if !validHLSManifestURL(child) {
			return nil, errInvalidHLSManifest
		}
		boundary := ""
		if segment.Number > 0 {
			// Each TS segment uses a fresh muxer and resets continuity
			// counters. RFC 8216 sections 3 and 4.3.2.3 require the playlist
			// to mark this discontinuity, independently of TS indicators.
			boundary = "#EXT-X-DISCONTINUITY\n"
		}
		duration := fmt.Sprintf("#EXTINF:%d.%07d,\n", segment.DurationTicks/media.TicksPerSecond, segment.DurationTicks%media.TicksPerSecond)
		if len(boundary)+len(duration)+len(child)+1+len(endList) > maxHLSManifestBytes-result.Len() {
			return nil, errHLSManifestLimit
		}
		result.WriteString(boundary)
		result.WriteString(duration)
		result.WriteString(child)
		result.WriteByte('\n')
	}
	result.WriteString(endList)
	return []byte(result.String()), nil
}

// validHLSManifestURL accepts path-relative and single-slash root-relative URI
// references. It preserves the original query bytes, including credentials.
// ASCII URI text avoids playlist whitespace, control characters, and Unicode
// normalization ambiguity; percent-encoded UTF-8 path components remain valid.
func validHLSManifestURL(value string) bool {
	if value == "" || len(value) > maxHLSManifestURLBytes || strings.ContainsAny(value, "#\\\"<>^`{|}") {
		return false
	}
	for _, char := range value {
		if char <= 0x20 || char >= 0x7f {
			return false
		}
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.User != nil || parsed.Opaque != "" || parsed.Fragment != "" || parsed.Path == "" {
		return false
	}
	// Reject encoded separators that reveal traversal after decoding, as well
	// as nested escapes that a proxy or handler could decode a second time.
	if !safeHLSDecodedText(parsed.Path) || strings.ContainsAny(parsed.Path, "\\%:") || strings.Contains(parsed.Path, "//") {
		return false
	}
	for _, component := range strings.Split(parsed.Path, "/") {
		if component == "." || component == ".." {
			return false
		}
	}
	query, err := url.QueryUnescape(parsed.RawQuery)
	return err == nil && safeHLSDecodedText(query)
}

func safeHLSDecodedText(value string) bool {
	if !utf8.ValidString(value) {
		return false
	}
	for _, char := range value {
		if char < 0x20 || (char >= 0x7f && char <= 0x9f) || char == '\ufeff' {
			return false
		}
	}
	return true
}
