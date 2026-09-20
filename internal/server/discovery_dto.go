package server

import "github.com/moooyo/goby/internal/library"

// Discovery fields describe only retained catalog/probe facts. They do not
// promise that a particular client can decode a stream or grant media access.
func addDiscoveryItemFields(dto map[string]any, item library.Item, fields []string, detail bool) {
	if item.ExpectedEpisode != nil {
		dto["LocationType"] = "Virtual"
		dto["MediaType"] = "Video"
		dto["IsMissing"], dto["IsPlaceHolder"] = true, true
		dto["IsVirtualUnaired"], dto["IsUnaired"] = item.ExpectedEpisode.IsUnaired, item.ExpectedEpisode.IsUnaired
		dto["CanDownload"], dto["SupportsResume"] = false, false
		return
	}
	if detail || hasField(fields, "LocationType") {
		if item.Path != "" {
			dto["LocationType"] = "FileSystem"
		}
	}
	if item.Media == nil {
		return
	}
	if item.Media.Size > 0 && (detail || hasField(fields, "Size")) {
		dto["Size"] = item.Media.Size
	}
	if item.Media.Bitrate > 0 && (detail || hasField(fields, "Bitrate")) {
		dto["Bitrate"] = item.Media.Bitrate
	}
	videoSeen, audioSeen := false, false
	for _, stream := range item.Media.Streams {
		if stream.CodecType == "video" && !stream.IsAttachedPicture && !videoSeen {
			videoSeen = true
			if stream.Width > 0 && (detail || hasField(fields, "Width")) {
				dto["Width"] = stream.Width
			}
			if stream.Height > 0 && (detail || hasField(fields, "Height")) {
				dto["Height"] = stream.Height
			}
			if stream.Codec != "" && (detail || hasField(fields, "VideoCodec")) {
				dto["VideoCodec"] = stream.Codec
			}
		}
		if stream.CodecType == "audio" && !audioSeen {
			audioSeen = true
			if stream.Codec != "" && (detail || hasField(fields, "AudioCodec")) {
				dto["AudioCodec"] = stream.Codec
			}
		}
	}
}
