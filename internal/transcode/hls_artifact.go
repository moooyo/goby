package transcode

import (
	"strconv"
	"strings"
)

// HLSArtifact classifies a public, generated file in an HLS job directory.
// Private publication files, arbitrary paths, and unrecognized variants are
// deliberately excluded. The legacy unpadded TS names remain available to cache
// callers; generated playlists use the stricter sequence parser below.
func HLSArtifact(name string) (kind string, ok bool) {
	switch name {
	case "main.m3u8", "v0.m3u8", "v1.m3u8", "v2.m3u8", "v3.m3u8":
		return "playlist", true
	case "init.mp4", "v0-init.mp4", "v1-init.mp4", "v2-init.mp4", "v3-init.mp4":
		return "init", true
	}
	if len(name) <= 255 && strings.HasPrefix(name, "segment-") && strings.HasSuffix(name, ".ts") {
		digits := name[len("segment-") : len(name)-len(".ts")]
		if asciiDigits(digits) {
			return "segment", true
		}
	}
	_, _, extension, valid := generatedHLSSegment(name)
	if !valid {
		return "", false
	}
	if extension == "vtt" {
		return "subtitle", true
	}
	return "segment", true
}

func generatedHLSSegment(name string) (number int64, rendition, extension string, ok bool) {
	if len(name) > 3 && name[0] == 'v' && name[1] >= '0' && name[1] <= '3' && name[2] == '-' {
		rendition, name = name[:2], name[3:]
	}
	if !strings.HasPrefix(name, "segment-") {
		return 0, "", "", false
	}
	digits, extension, found := strings.Cut(strings.TrimPrefix(name, "segment-"), ".")
	if !found || len(digits) < 6 || len(digits) > 10 || !asciiDigits(digits) {
		return 0, "", "", false
	}
	switch extension {
	case "ts", "m4s", "aac", "mp3", "vtt":
	default:
		return 0, "", "", false
	}
	number, err := strconv.ParseInt(digits, 10, 32)
	if err != nil {
		return 0, "", "", false
	}
	return number, rendition, extension, true
}

func asciiDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func renditionInitName(rendition string) string {
	if rendition == "" {
		return "init.mp4"
	}
	return rendition + "-init.mp4"
}
