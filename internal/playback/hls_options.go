package playback

import (
	"sort"
	"strconv"
	"strings"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

func hlsProfileContainer(profile TranscodingProfile, kind DlnaProfileType) string {
	if strings.TrimSpace(profile.Container) == "" {
		return "ts"
	}
	for _, value := range strings.Split(profile.Container, ",") {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "ts", "mpegts", "mpeg-ts":
			return "ts"
		case "mp4", "fmp4":
			return "mp4"
		case "aac", "adts":
			if kind == DlnaProfileTypeAudio {
				return "aac"
			}
		case "mp3":
			if kind == DlnaProfileTypeAudio {
				return "mp3"
			}
		}
	}
	return ""
}

func configureHLSSubtitle(plan *transcode.Plan, source Source, request Request, profile TranscodingProfile, subtitle *media.Stream, videoCopy bool) *Reason {
	if subtitle == nil {
		return nil
	}
	fail := func() *Reason {
		return conversionReason("conversion_subtitle_unsupported", "SubtitleStreamIndex", "The selected subtitle requires an explicitly supported HLS, external, or encoded delivery method.")
	}
	deliverySource := source
	deliverySource.Info.Container, deliverySource.Path = plan.Container, "output."+plan.Container
	method, _ := selectSubtitleDelivery(deliverySource, request.DeviceProfile, subtitle)
	if method == SubtitleDeliveryMethodExternal && profile.ManifestSubtitles == "" {
		return nil
	}
	selected := SubtitleDeliveryMethod("")
	for _, candidate := range request.DeviceProfile.SubtitleProfiles {
		if !matchesList(candidate.Language, subtitle.Language) || !matchesList(candidate.Container, plan.Container) ||
			candidate.Protocol != "" && !strings.EqualFold(candidate.Protocol, "hls") {
			continue
		}
		if candidate.Method == SubtitleDeliveryMethodHls && subtitle.IsTextSubtitleStream && (matchesList(candidate.Format, "vtt") || matchesList(candidate.Format, "webvtt")) {
			selected = candidate.Method
			break
		}
		if candidate.Method == SubtitleDeliveryMethodEncode && matchesList(candidate.Format, subtitle.Codec) {
			selected = candidate.Method
			break
		}
	}
	if profile.ManifestSubtitles != "" && subtitle.IsTextSubtitleStream {
		selected = SubtitleDeliveryMethodHls
	}
	if selected == "" {
		return fail()
	}
	if selected == SubtitleDeliveryMethodEncode && (videoCopy || plan.VideoStreamIndex < 0) {
		return fail()
	}
	plan.Subtitle = transcode.SubtitlePlan{Mode: "hls", Codec: subtitle.Codec, StreamIndex: subtitle.Index}
	if subtitle.IsExternal {
		if subtitle.SubtitleTag == "" {
			return fail()
		}
		plan.Subtitle.ExternalTag = subtitle.SubtitleTag
	}
	for _, stream := range source.Info.Streams {
		if stream.CodecType == "subtitle" && !stream.IsExternal && stream.Index < subtitle.Index {
			plan.Subtitle.SubtitleOrdinal++
		}
	}
	if selected == SubtitleDeliveryMethodEncode {
		plan.Subtitle.Mode, plan.Hardware = "burn", transcode.Hardware{}
		var indexes []int
		for _, stream := range source.Info.Streams {
			if stream.CodecType == "attachment" && media.FontAttachment(stream) {
				indexes = append(indexes, stream.Index)
			}
		}
		sort.Ints(indexes)
		if len(indexes) > 16 {
			return fail()
		}
		if !transcode.IsBitmapSubtitle(subtitle.Codec) {
			values := make([]string, len(indexes))
			for index, stream := range indexes {
				values[index] = strconv.Itoa(stream)
			}
			plan.Subtitle.FontStreams = strings.Join(values, ",")
		}
	}
	return nil
}

func configureHLSRenditions(plan *transcode.Plan, request Request, profile TranscodingProfile, projected Source, kind DlnaProfileType) *Reason {
	if plan.VideoCodec != "h264" || plan.Width < 4 || plan.Height < 4 || plan.VideoBitrate < 128000 {
		return conversionReason("conversion_adaptive_video_required", "EnableAdaptiveBitrate", "Adaptive HLS requires video encoding and at least two compatible renditions.")
	}
	plan.HLS.Renditions[0] = transcode.HLSRendition{Width: plan.Width, Height: plan.Height, VideoBitrate: plan.VideoBitrate}
	plan.HLS.RenditionCount = 1
	for _, scale := range []float64{0.75, 0.5, 0.25} {
		candidate := transcode.HLSRendition{Width: max(2, int(float64(plan.Width)*scale)/2*2), Height: max(2, int(float64(plan.Height)*scale)/2*2), VideoBitrate: max(64000, int64(float64(plan.VideoBitrate)*scale*scale))}
		prior := plan.HLS.Renditions[plan.HLS.RenditionCount-1]
		if candidate.Width >= prior.Width || candidate.Height >= prior.Height || candidate.VideoBitrate >= prior.VideoBitrate {
			continue
		}
		output := projected
		output.Info.Streams = append([]media.Stream(nil), projected.Info.Streams...)
		for index := range output.Info.Streams {
			if output.Info.Streams[index].Index == plan.VideoStreamIndex {
				output.Info.Streams[index].Width, output.Info.Streams[index].Height, output.Info.Streams[index].Bitrate = candidate.Width, candidate.Height, candidate.VideoBitrate
			}
		}
		output.Info.Bitrate = (candidate.VideoBitrate + max(plan.AudioBitrate, projected.Info.Bitrate*9/10-plan.VideoBitrate)) * 10 / 9
		outputRequest := conversionOutputRequest(request, profile, kind)
		if plan.Subtitle.Mode != "" {
			disabled := -1
			outputRequest.SubtitleStreamIndex = &disabled
		}
		decision, err := Evaluate(output, outputRequest)
		if err != nil || !decision.OriginalCompatible || !decision.ProfileMatched {
			continue
		}
		plan.HLS.Renditions[plan.HLS.RenditionCount] = candidate
		plan.HLS.RenditionCount++
	}
	if plan.HLS.RenditionCount < 2 {
		return conversionReason("conversion_adaptive_ladder_unavailable", "EnableAdaptiveBitrate", "The client constraints do not allow two distinct encoded renditions.")
	}
	if plan.HLS.SegmentType == "" {
		plan.HLS.SegmentType = "mpegts"
	}
	return nil
}
