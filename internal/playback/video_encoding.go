package playback

import (
	"strings"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

// videoEncodingFormat describes facts the encoder can establish. Codec level
// and reference-frame counts remain unknown until the output is inspected.
type videoEncodingFormat struct {
	codec, profile, videoRange string
	bitDepth                   int
}

func videoEncodingFormats(selector, container string) []videoEncodingFormat {
	var result []videoEncodingFormat
	codecs := audioProfileSelectors(selector)
	if strings.TrimSpace(selector) == "" {
		codecs = []string{"h264"}
	}
	for _, codec := range codecs {
		if !videoCodecContainerSupported(codec, container) {
			continue
		}
		var formats []videoEncodingFormat
		switch codec {
		case "h264":
			formats = []videoEncodingFormat{{codec: codec, profile: "high", bitDepth: 8}, {codec: codec, profile: "main", bitDepth: 8}, {codec: codec, profile: "baseline", bitDepth: 8}}
		case "hevc":
			formats = []videoEncodingFormat{{codec: codec, profile: "main", bitDepth: 8}, {codec: codec, profile: "main10", bitDepth: 10}}
		case "av1":
			formats = []videoEncodingFormat{{codec: codec, profile: "main", bitDepth: 8}, {codec: codec, profile: "main", bitDepth: 10}}
		}
		result = append(result, formats...)
		for _, format := range formats {
			if format.bitDepth == 10 {
				format.videoRange = "HDR10"
				result = append(result, format)
			}
		}
	}
	return result
}

func videoCodecContainerSupported(codec, container string) bool {
	return transcode.VideoEncodingSupported(codec) && (container == "mp4" || container == "ts" && codec != "av1")
}

func progressiveVideoEncoding(request ProgressiveVideoRequest) (videoEncodingFormat, bool) {
	for _, format := range videoEncodingFormats(strings.ToLower(request.VideoCodec), "mp4") {
		if request.VideoProfile != "" && !strings.EqualFold(request.VideoProfile, format.profile) ||
			request.VideoBitDepth != nil && *request.VideoBitDepth != format.bitDepth ||
			strings.EqualFold(request.VideoRange, "HDR10") != (format.videoRange == "HDR10") {
			continue
		}
		return format, true
	}
	return videoEncodingFormat{}, false
}

func videoEncodingProfileName(profile string) string {
	switch strings.ToLower(profile) {
	case "baseline":
		return "Baseline"
	case "main":
		return "Main"
	case "main10":
		return "Main 10"
	case "high":
		return "High"
	}
	return ""
}

func videoEncodedOutput(video *media.Stream, plan transcode.Plan) {
	video.Codec, video.BitDepth = plan.VideoCodec, transcode.VideoOutputBitDepth(plan)
	video.Profile, video.Level, video.RefFrames = videoEncodingProfileName(plan.VideoProfile), 0, 0
	video.PixelFormat = "yuv420p"
	if video.BitDepth == 10 {
		video.PixelFormat = "yuv420p10le"
	}
	video.ColorRange, video.ColorSpace, video.ColorTransfer, video.ColorPrimaries = "", "", "", ""
	videoProcessingOutput(video, plan.VideoFilters)
}

func videoOutputFraming(video *media.Stream, plan transcode.Plan) {
	container := plan.Container
	video.IsAVC, video.IsAVCKnown = video.Codec == "h264" && container == "mp4", true
	video.CodecTag, video.CodecTagString, video.TimeBase = "", "", ""
	if container == "mp4" {
		video.CodecTag, video.CodecTagString = transcode.VideoMP4Tag(plan), transcode.VideoMP4Tag(plan)
	}
}

type conversionEncodingCandidate struct {
	profile TranscodingProfile
	video   videoEncodingFormat
}

// Expand only implemented container/codec/profile combinations, preserving the
// client's selector order. A failed TS choice must not hide an offered fMP4 path.
func conversionEncodingCandidates(profile TranscodingProfile, kind DlnaProfileType, videoCopy, audioCopy bool) []conversionEncodingCandidate {
	var result []conversionEncodingCandidate
	containers := audioProfileSelectors(conversionContainerSelector(profile.Container))
	if len(containers) == 0 {
		containers = []string{"ts"}
	}
	for _, container := range containers {
		base := profile
		base.Container = container
		if hlsProfileContainer(base, kind) == "" {
			continue
		}
		for _, audio := range conversionAudioProfiles(base, audioCopy) {
			if kind != DlnaProfileTypeVideo || videoCopy {
				result = append(result, conversionEncodingCandidate{profile: audio})
				continue
			}
			for _, format := range videoEncodingFormats(profile.VideoCodec, container) {
				candidate := audio
				candidate.VideoCodec = format.codec
				result = append(result, conversionEncodingCandidate{profile: candidate, video: format})
			}
		}
	}
	return result
}
