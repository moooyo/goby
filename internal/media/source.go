package media

import (
	"path/filepath"
	"strings"
)

// SourceID is the shared public identifier for an item's original local file.
// It does not identify a playback session or grant access to the source.
func SourceID(itemID string) string {
	return "mediasource_" + itemID
}

// CanonicalContainer resolves ffprobe's format aliases using a compatible file
// extension. The extension never overrides an unrelated probed container.
func CanonicalContainer(info Info, path string) string {
	formats := strings.Split(strings.ToLower(info.Container), ",")
	for index := range formats {
		formats[index] = strings.TrimSpace(formats[index])
	}
	has := func(name string) bool {
		for _, format := range formats {
			if format == name {
				return true
			}
		}
		return false
	}
	extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if has("matroska") || has("webm") {
		if extension == "webm" && has("webm") {
			return "webm"
		}
		if has("matroska") {
			if extension == "mka" {
				return "mka"
			}
			return "mkv"
		}
		return "webm"
	}
	if has("mov") || has("mp4") {
		switch extension {
		case "mov", "mp4", "m4a", "m4v", "3gp", "3g2", "mj2":
			return extension
		}
	}
	if has("mpegts") {
		if extension == "m2ts" || extension == "mts" {
			return extension
		}
		return "ts"
	}
	if has(extension) && extension != "" {
		return extension
	}
	if len(formats) != 0 {
		return formats[0]
	}
	return ""
}

// SourceMIMEType describes the original file without opening it. Unknown
// containers use application/octet-stream rather than a guessed media type.
func SourceMIMEType(info Info, path string) string {
	var hasAudio, hasVideo bool
	for _, stream := range info.Streams {
		if strings.EqualFold(stream.CodecType, "video") && !stream.IsAttachedPicture {
			hasVideo = true
		}
		if strings.EqualFold(stream.CodecType, "audio") {
			hasAudio = true
		}
	}
	if !hasAudio && !hasVideo {
		return "application/octet-stream"
	}
	audioOnly := hasAudio && !hasVideo
	switch CanonicalContainer(info, path) {
	case "mp4", "m4v", "m4a", "mov", "mj2":
		if audioOnly {
			return "audio/mp4"
		}
		if CanonicalContainer(info, path) == "mov" {
			return "video/quicktime"
		}
		return "video/mp4"
	case "mkv", "mka":
		if audioOnly {
			return "audio/x-matroska"
		}
		return "video/x-matroska"
	case "webm":
		if audioOnly {
			return "audio/webm"
		}
		return "video/webm"
	case "mp3":
		return "audio/mpeg"
	case "flac":
		return "audio/flac"
	case "wav":
		return "audio/wav"
	case "ogg", "oga", "opus":
		if audioOnly {
			return "audio/ogg"
		}
		return "video/ogg"
	case "aac":
		return "audio/aac"
	case "ts", "m2ts", "mts", "mpegts":
		return "video/mp2t"
	case "mpeg", "mpg":
		return "video/mpeg"
	case "avi":
		return "video/x-msvideo"
	case "3gp":
		if audioOnly {
			return "audio/3gpp"
		}
		return "video/3gpp"
	case "3g2":
		if audioOnly {
			return "audio/3gpp2"
		}
		return "video/3gpp2"
	default:
		return "application/octet-stream"
	}
}
