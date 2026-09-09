package transcode

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

var ErrInvalidPlaylist = errors.New("invalid generated HLS playlist")

const (
	MaxPlaylistBytes    = 1024 * 1024
	MaxPlaylistSegments = 16_384
)

type MediaSegment struct {
	Number        int64
	Name          string
	DurationTicks int64
	Discontinuity bool
}

// MediaPlaylist represents actual published FFmpeg output. It does not predict
// stream-copy segment lengths or claim that an unfinished event is a full VOD.
type MediaPlaylist struct {
	Version        int
	TargetDuration int
	Sequence       int64
	Type           string
	Independent    bool
	Ended          bool
	Segments       []MediaSegment
}

// ParseMediaPlaylist accepts the bounded MPEG-TS subset emitted by this runner.
// Every URI must name a generated segment in the same job directory. Encryption,
// maps, alternate playlists, arbitrary paths, and external URIs are rejected.
func ParseMediaPlaylist(data []byte) (MediaPlaylist, error) {
	invalid := func() (MediaPlaylist, error) { return MediaPlaylist{}, ErrInvalidPlaylist }
	if len(data) == 0 || len(data) > MaxPlaylistBytes || !utf8.Valid(data) || bytes.ContainsRune(data, '\x00') {
		return invalid()
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	if lines[0] != "#EXTM3U" {
		return invalid()
	}
	playlist := MediaPlaylist{Version: 1, Segments: make([]MediaSegment, 0)}
	seen := map[string]bool{}
	var pendingDuration int64
	var discontinuity bool
	for _, line := range lines[1:] {
		if line == "" {
			continue
		}
		if len(line) > 2048 || strings.ContainsAny(line, "\r\t") || playlist.Ended {
			return invalid()
		}
		if !strings.HasPrefix(line, "#") {
			number, ok := segmentNumber(line)
			if !ok || pendingDuration <= 0 || len(playlist.Segments) >= MaxPlaylistSegments ||
				number != playlist.Sequence+int64(len(playlist.Segments)) {
				return invalid()
			}
			playlist.Segments = append(playlist.Segments, MediaSegment{Number: number, Name: line, DurationTicks: pendingDuration, Discontinuity: discontinuity})
			pendingDuration, discontinuity = 0, false
			continue
		}
		name, value, hasValue := strings.Cut(line, ":")
		switch name {
		case "#EXTINF":
			if pendingDuration != 0 || !hasValue {
				return invalid()
			}
			duration, _, comma := strings.Cut(value, ",")
			var ok bool
			pendingDuration, ok = playlistDurationTicks(duration)
			if !comma || !ok {
				return invalid()
			}
		case "#EXT-X-DISCONTINUITY":
			if hasValue || discontinuity {
				return invalid()
			}
			discontinuity = true
		case "#EXT-X-ENDLIST":
			if hasValue || pendingDuration != 0 || discontinuity {
				return invalid()
			}
			playlist.Ended = true
		case "#EXT-X-VERSION", "#EXT-X-TARGETDURATION", "#EXT-X-MEDIA-SEQUENCE", "#EXT-X-PLAYLIST-TYPE", "#EXT-X-INDEPENDENT-SEGMENTS":
			if seen[name] || len(playlist.Segments) != 0 || pendingDuration != 0 {
				return invalid()
			}
			seen[name] = true
			if name == "#EXT-X-INDEPENDENT-SEGMENTS" {
				if hasValue {
					return invalid()
				}
				playlist.Independent = true
				continue
			}
			if !hasValue {
				return invalid()
			}
			if name == "#EXT-X-PLAYLIST-TYPE" {
				if value != "EVENT" && value != "VOD" {
					return invalid()
				}
				playlist.Type = value
				continue
			}
			number, err := strconv.ParseInt(value, 10, 32)
			if err != nil || number < 0 || strconv.FormatInt(number, 10) != value {
				return invalid()
			}
			switch name {
			case "#EXT-X-VERSION":
				if number < 1 || number > 9 {
					return invalid()
				}
				playlist.Version = int(number)
			case "#EXT-X-TARGETDURATION":
				if number < 1 || number > 86_400 {
					return invalid()
				}
				playlist.TargetDuration = int(number)
			case "#EXT-X-MEDIA-SEQUENCE":
				playlist.Sequence = number
			}
		default:
			// The encoder's closed output format does not need arbitrary tags.
			// Rejecting unknown URI-bearing features is safer than copying them.
			return invalid()
		}
	}
	if playlist.TargetDuration == 0 || pendingDuration != 0 || discontinuity || len(playlist.Segments) == 0 ||
		(playlist.Type == "VOD" && !playlist.Ended) {
		return invalid()
	}
	for _, segment := range playlist.Segments {
		// RFC 8216 compares EXTINF rounded to the nearest integer with the
		// target duration; the actual decimal duration remains unchanged.
		if (segment.DurationTicks+ticksPerSecond/2)/ticksPerSecond > int64(playlist.TargetDuration) {
			return invalid()
		}
	}
	return playlist, nil
}

// RewriteMediaPlaylist keeps measured durations, sequence and completion state,
// replacing only generated file names with authorized relative child URLs. It
// never adds ENDLIST or independent-segment claims to unfinished/copied output.
func RewriteMediaPlaylist(data []byte, childURL func(MediaSegment) string) ([]byte, error) {
	playlist, err := ParseMediaPlaylist(data)
	if err != nil || childURL == nil {
		return nil, ErrInvalidPlaylist
	}
	var result strings.Builder
	fmt.Fprintf(&result, "#EXTM3U\n#EXT-X-VERSION:%d\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:%d\n", playlist.Version, playlist.TargetDuration, playlist.Sequence)
	if playlist.Type != "" {
		fmt.Fprintf(&result, "#EXT-X-PLAYLIST-TYPE:%s\n", playlist.Type)
	}
	if playlist.Independent {
		result.WriteString("#EXT-X-INDEPENDENT-SEGMENTS\n")
	}
	for _, segment := range playlist.Segments {
		child := childURL(segment)
		if !validPlaylistChildURL(child) {
			return nil, ErrInvalidPlaylist
		}
		if segment.Discontinuity {
			result.WriteString("#EXT-X-DISCONTINUITY\n")
		}
		fmt.Fprintf(&result, "#EXTINF:%d.%07d,\n%s\n", segment.DurationTicks/ticksPerSecond, segment.DurationTicks%ticksPerSecond, child)
		if result.Len() > MaxPlaylistBytes {
			return nil, ErrInvalidPlaylist
		}
	}
	if playlist.Ended {
		result.WriteString("#EXT-X-ENDLIST\n")
	}
	return []byte(result.String()), nil
}

func segmentNumber(name string) (int64, bool) {
	if !strings.HasPrefix(name, "segment-") || !strings.HasSuffix(name, ".ts") {
		return 0, false
	}
	number := strings.TrimSuffix(strings.TrimPrefix(name, "segment-"), ".ts")
	if len(number) < 6 || len(number) > 10 {
		return 0, false
	}
	for _, char := range number {
		if char < '0' || char > '9' {
			return 0, false
		}
	}
	index, err := strconv.ParseInt(number, 10, 32)
	return index, err == nil
}

func playlistDurationTicks(value string) (int64, bool) {
	whole, fraction, _ := strings.Cut(value, ".")
	if whole == "" || len(fraction) > 7 || len(whole) > 6 {
		return 0, false
	}
	for _, char := range whole + fraction {
		if char < '0' || char > '9' {
			return 0, false
		}
	}
	seconds, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || seconds > 86_400 {
		return 0, false
	}
	subseconds, err := strconv.ParseInt(fraction+strings.Repeat("0", 7-len(fraction)), 10, 64)
	ticks := seconds*ticksPerSecond + subseconds
	return ticks, err == nil && ticks > 0
}

func validPlaylistChildURL(value string) bool {
	if value == "" || len(value) > 8192 || !utf8.ValidString(value) || strings.ContainsAny(value, "\r\n\t\x00\\\"") {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || parsed.Fragment != "" || parsed.Path == "" {
		return false
	}
	for _, component := range strings.Split(parsed.Path, "/") {
		if component == "." || component == ".." {
			return false
		}
	}
	return true
}
