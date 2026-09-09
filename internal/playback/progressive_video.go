package playback

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

// ProgressiveVideoRequest contains normalized media requirements, not HTTP
// defaults. Exact targets and ceilings are independent. An explicit FrameRate
// requests a real frame conversion; an omitted rate preserves timestamp pacing.
// Format-clock facts never belong here: only the verified source supplies them.
type ProgressiveVideoRequest struct {
	OutputContainer, VideoCodec, AudioCodec                         string
	VideoStreamIndex, AudioStreamIndex                              *int
	StartTimeTicks                                                  int64
	VideoBitrate, AudioBitrate, MaxBitrate                          *int64
	MaxVideoBitrate, MaxAudioBitrate                                *int64
	Width, Height, MaxWidth, MaxHeight                              *int
	FrameRate, MaxFrameRate                                         *float64
	AudioChannels, AudioSampleRate, MaxAudioChannels, MaxSampleRate *int
	AllowVideoStreamCopy, AllowAudioStreamCopy                      *bool
	AllowInterlacedVideoStreamCopy                                  *bool
}

// ProgressiveVideoDecision describes an immutable MP4 conversion. Stream
// indexes in Plan refer to the source; projected output indexes are video 0 and
// audio 1. Bitrates are source declarations or encoder targets, not measured
// network throughput. DurationTicks describes the requested presentation window.
type ProgressiveVideoDecision struct {
	Plan         *transcode.Plan
	OutputSource Source
	Method       string
	Reasons      []Reason
}

// PlanProgressiveVideo plans bounded H.264/AAC fragmented MP4 without starting
// a worker or granting source access. Copy seeks are deliberately unavailable
// until a client-independent pre-roll contract is verified. Valid unsupported
// requirements return nil Plan and reasons; malformed input returns an error.
func PlanProgressiveVideo(source Source, request ProgressiveVideoRequest, limits ConversionLimits) (ProgressiveVideoDecision, error) {
	var result ProgressiveVideoDecision
	if err := validateProgressiveVideoRequest(request); err != nil {
		return result, err
	}
	selectionRequest := Request{AudioStreamIndex: request.AudioStreamIndex}
	if err := validateRequest(source, selectionRequest); err != nil {
		return result, err
	}
	selection, err := selectStreams(source, selectionRequest)
	if err != nil {
		return result, err
	}
	if request.VideoStreamIndex != nil {
		selection.video = nil
		for index := range source.Info.Streams {
			stream := &source.Info.Streams[index]
			if stream.Index == *request.VideoStreamIndex && strings.EqualFold(stream.CodecType, "video") && !stream.IsAttachedPicture {
				selection.video = stream
				break
			}
		}
		if selection.video == nil {
			return result, fmt.Errorf("%w: selected video stream does not exist", ErrInvalidRequest)
		}
	}
	limits, err = normalizeConversionLimits(limits)
	if err != nil {
		return result, err
	}
	decline := func(code, property, message string) (ProgressiveVideoDecision, error) {
		result.Reasons = appendConversionReasons(result.Reasons, *conversionReason(code, property, message))
		return result, nil
	}
	if sourceKind(source, selection) != DlnaProfileTypeVideo || selection.video == nil || selection.video.IsExternal || selection.audio != nil && selection.audio.IsExternal {
		return decline("progressive_video_source_unsupported", "Type", "Progressive video requires internal local video and audio tracks.")
	}
	if !source.Info.FormatStartKnown || source.Info.FormatStartTicks < -progressiveMaxDuration || source.Info.FormatStartTicks > progressiveMaxDuration {
		return decline("progressive_video_origin_unverified", "FormatStartTicks", "The source must have a verified bounded format-clock origin.")
	}
	if source.Info.DurationTicks <= 0 || source.Info.DurationTicks > progressiveMaxDuration || request.StartTimeTicks >= source.Info.DurationTicks {
		return decline("progressive_video_duration_unsupported", "StartTimeTicks", "Video conversion requires a positive bounded remaining presentation.")
	}
	video := *selection.video
	if video.Width <= 0 || video.Height <= 0 || video.Width > 65536 || video.Height > 65536 || video.Bitrate < 0 || video.BitDepth < 0 {
		return decline("progressive_video_facts_missing", "Width", "Known bounded video dimensions and valid source facts are required.")
	}
	if video.IsInterlaced || conversionHDR(&video) {
		return decline("progressive_video_range_unsupported", "VideoRange", "This MP4 pipeline does not implement deinterlacing or HDR tone mapping.")
	}
	container, videoCodec, audioCodec := strings.ToLower(request.OutputContainer), strings.ToLower(request.VideoCodec), strings.ToLower(request.AudioCodec)
	if container != "mp4" || videoCodec != "h264" && videoCodec != "copy" || audioCodec != "" && audioCodec != "none" && audioCodec != "aac" && audioCodec != "copy" {
		return decline("progressive_video_format_unsupported", "OutputContainer", "The selected video conversion format is not implemented.")
	}
	if selection.audio == nil {
		if request.AudioChannels != nil || request.AudioSampleRate != nil || request.AudioBitrate != nil {
			return decline("progressive_video_audio_missing", "AudioStreamIndex", "Audio transformation targets cannot create a missing source audio track.")
		}
	} else {
		if audioCodec == "" || audioCodec == "none" {
			return decline("progressive_video_audio_drop_unsupported", "AudioCodec", "Existing audio cannot be silently dropped by this conversion request.")
		}
		if selection.audio.Channels <= 0 || selection.audio.Channels > 256 || selection.audio.SampleRate <= 0 || selection.audio.SampleRate > math.MaxInt32 || selection.audio.Bitrate < 0 || selection.audio.BitDepth < 0 {
			return decline("progressive_video_audio_facts_missing", "AudioStreamIndex", "Known audio channels and sample rate are required to plan the selected track.")
		}
	}
	totalBudget := limits.MaxBitrate
	if request.MaxBitrate != nil {
		totalBudget = min(totalBudget, *request.MaxBitrate)
	}
	for _, mode := range [][2]bool{{true, true}, {true, false}, {false, true}, {false, false}} {
		videoCopy, audioCopy := mode[0], mode[1]
		if selection.audio == nil {
			if !audioCopy {
				continue
			}
		}
		if videoCopy && (isFalse(request.AllowVideoStreamCopy) || request.StartTimeTicks != 0 || request.FrameRate != nil) ||
			!videoCopy && (!limits.AllowVideoTranscode || videoCodec == "copy") ||
			selection.audio != nil && (audioCopy && isFalse(request.AllowAudioStreamCopy) || !audioCopy && (!limits.AllowAudioTranscode || audioCodec == "copy")) ||
			videoCopy && audioCopy && !limits.AllowRemux {
			continue
		}
		plan := transcode.Plan{OutputMode: "progressive", Container: "mp4", VideoStreamIndex: video.Index, AudioStreamIndex: -1,
			StartTicks: request.StartTimeTicks, DurationTicks: source.Info.DurationTicks,
			SourceFormatStartKnown: source.Info.FormatStartKnown, SourceFormatStartTicks: source.Info.FormatStartTicks}
		projectedVideo := video
		videoCost, videoReason := progressiveVideoTrack(&plan, &projectedVideo, source.Info.Bitrate, request, limits, videoCopy)
		if videoReason != nil {
			result.Reasons = appendConversionReasons(result.Reasons, *videoReason)
			continue
		}
		reservedVideo := int64(64_000)
		if videoCopy || request.VideoBitrate != nil {
			reservedVideo = videoCost
		}
		if reservedVideo > totalBudget {
			continue
		}
		audioPlans := []progressiveVideoAudio{{}}
		if selection.audio != nil {
			audioPlans = progressiveVideoAudioTracks(*selection.audio, source.Info.Bitrate, request, limits, audioCopy, totalBudget-reservedVideo)
		}
		for _, audio := range audioPlans {
			candidate, outputVideo := plan, projectedVideo
			if !videoCopy {
				maximum := min(int64(200_000_000), totalBudget-audio.cost)
				if request.MaxVideoBitrate != nil {
					maximum = min(maximum, *request.MaxVideoBitrate)
				}
				if maximum < 64_000 || request.VideoBitrate != nil && *request.VideoBitrate > maximum {
					continue
				}
				candidate.VideoBitrate = min(videoCost, maximum)
				if candidate.VideoBitrate < 64_000 {
					continue
				}
				outputVideo.Bitrate = candidate.VideoBitrate
			}
			plannedVideoRate := videoCost
			if !videoCopy {
				plannedVideoRate = candidate.VideoBitrate
			}
			if plannedVideoRate > totalBudget-audio.cost {
				continue
			}
			streams := []media.Stream{outputVideo}
			if audio.present {
				candidate.AudioStreamIndex, candidate.AudioCodec = selection.audio.Index, audio.codec
				candidate.AudioBitrate, candidate.AudioChannels, candidate.AudioSampleRate = audio.bitrate, audio.channels, audio.rate
				streams = append(streams, audio.output)
			}
			if transcode.ValidatePlan(candidate) != nil {
				continue
			}
			result.Plan, result.Method = &candidate, "DirectStream"
			if !videoCopy || !audioCopy {
				result.Method = "Transcode"
			}
			result.OutputSource = Source{ItemID: source.ItemID, MediaSourceID: source.MediaSourceID, ItemType: source.ItemType, Path: "output.mp4",
				Info: media.Info{Container: "mp4", DurationTicks: source.Info.DurationTicks - request.StartTimeTicks,
					Bitrate: plannedVideoRate + audio.cost, Streams: streams}}
			result.Reasons = nil
			return result, nil
		}
	}
	return decline("progressive_video_no_output", "VideoCodec", "No authorized MP4 conversion satisfies the exact targets, copy restrictions, and media budget.")
}

func validateProgressiveVideoRequest(request ProgressiveVideoRequest) error {
	for _, value := range []string{request.OutputContainer, request.VideoCodec} {
		if value == "" || !progressiveVideoSelector(value) {
			return fmt.Errorf("%w: an explicit video container and codec are required", ErrInvalidRequest)
		}
	}
	if request.AudioCodec != "" && !progressiveVideoSelector(request.AudioCodec) || request.StartTimeTicks < 0 {
		return fmt.Errorf("%w: invalid video selector or start position", ErrInvalidRequest)
	}
	for _, value := range []*int{request.Width, request.Height, request.MaxWidth, request.MaxHeight, request.AudioChannels, request.AudioSampleRate, request.MaxAudioChannels, request.MaxSampleRate} {
		if value != nil && (*value <= 0 || *value > math.MaxInt32) {
			return fmt.Errorf("%w: video targets and ceilings must be positive int32 values", ErrInvalidRequest)
		}
	}
	for _, value := range []*int{request.VideoStreamIndex, request.AudioStreamIndex} {
		if value != nil && (*value < 0 || *value > math.MaxInt32) {
			return fmt.Errorf("%w: selected streams require nonnegative indexes", ErrInvalidRequest)
		}
	}
	for _, value := range []*int64{request.VideoBitrate, request.AudioBitrate, request.MaxBitrate, request.MaxVideoBitrate, request.MaxAudioBitrate} {
		if value != nil && *value <= 0 {
			return fmt.Errorf("%w: video bitrate targets and ceilings must be positive", ErrInvalidRequest)
		}
	}
	for _, value := range []*float64{request.FrameRate, request.MaxFrameRate} {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0) || *value <= 0) {
			return fmt.Errorf("%w: frame rates must be positive finite values", ErrInvalidRequest)
		}
	}
	return nil
}

func progressiveVideoSelector(value string) bool {
	if len(value) == 0 || len(value) > 32 {
		return false
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == '_') {
			return false
		}
	}
	return true
}

func progressiveVideoTrack(plan *transcode.Plan, output *media.Stream, sourceBitrate int64, request ProgressiveVideoRequest, limits ConversionLimits, copy bool) (int64, *Reason) {
	fail := func(property, message string) (int64, *Reason) {
		return 0, conversionReason("progressive_video_track_unsupported", property, message)
	}
	maxWidth, maxHeight := limits.MaxWidth, limits.MaxHeight
	if request.MaxWidth != nil {
		maxWidth = min(maxWidth, *request.MaxWidth)
	}
	if request.MaxHeight != nil {
		maxHeight = min(maxHeight, *request.MaxHeight)
	}
	bitrate := output.Bitrate
	if copy {
		if !strings.EqualFold(output.Codec, "h264") || !output.InterlaceKnown && isFalse(request.AllowInterlacedVideoStreamCopy) ||
			output.Width > maxWidth || output.Height > maxHeight || request.Width != nil && output.Width != *request.Width || request.Height != nil && output.Height != *request.Height {
			return fail("VideoCodec", "The source video cannot be copied with the requested codec, geometry, or interlace restriction.")
		}
		if request.MaxFrameRate != nil && (conversionFrameRate(output) <= 0 || conversionFrameRate(output) > *request.MaxFrameRate) {
			return fail("MaxFrameRate", "Copied video does not prove the requested frame-rate ceiling.")
		}
		if request.VideoBitrate != nil && (output.Bitrate <= 0 || output.Bitrate != *request.VideoBitrate) {
			return fail("VideoBitrate", "Copied video does not match the exact declared bitrate.")
		}
		if bitrate <= 0 {
			bitrate = sourceBitrate
		}
		if bitrate <= 0 || request.MaxVideoBitrate != nil && bitrate > *request.MaxVideoBitrate {
			return fail("MaxVideoBitrate", "Copied video lacks a usable bitrate budget or exceeds its ceiling.")
		}
		plan.VideoCodec = "copy"
	} else {
		width, height, ok := progressiveVideoDimensions(output.Width, output.Height, maxWidth, maxHeight, request.Width, request.Height)
		if !ok {
			return fail("Width", "The requested dimensions cannot be constructed within the video limits.")
		}
		plan.VideoCodec, plan.Width, plan.Height, plan.Hardware = "h264", width, height, limits.Hardware
		output.Codec, output.Width, output.Height = "h264", width, height
		output.Profile, output.Level, output.RefFrames = "", 0, 0
		output.BitDepth, output.PixelFormat = 8, "yuv420p"
		output.IsInterlaced, output.InterlaceKnown, output.FieldOrder = false, true, "progressive"
		output.ColorRange, output.ColorSpace, output.ColorTransfer, output.ColorPrimaries = "", "", "", ""
		fps := float64(0)
		if request.FrameRate != nil {
			fps = *request.FrameRate
			if !progressiveVideoExactFrameRate(fps) || request.MaxFrameRate != nil && fps > *request.MaxFrameRate {
				return fail("FrameRate", "The exact frame rate is outside the representable output range or requested ceiling.")
			}
		} else if request.MaxFrameRate != nil {
			fps = min(*request.MaxFrameRate, 240)
			if sourceRate := conversionFrameRate(output); sourceRate > 0 {
				fps = min(fps, sourceRate)
			}
			fps = math.Floor(fps*1_000_000) / 1_000_000
			if !progressiveVideoExactFrameRate(fps) {
				return fail("MaxFrameRate", "The requested frame-rate ceiling has no supported CFR output.")
			}
		}
		plan.FrameRate = fps
		output.AverageFrameRate, output.RealFrameRate = "", ""
		if fps > 0 {
			output.AverageFrameRate, output.RealFrameRate = strconv.FormatFloat(fps, 'f', -1, 64), strconv.FormatFloat(fps, 'f', -1, 64)
		}
		if request.VideoBitrate != nil {
			bitrate = *request.VideoBitrate
		} else {
			if bitrate <= 0 {
				bitrate = 4_000_000
			}
			bitrate = max(bitrate, 128_000)
		}
		if bitrate < 64_000 || request.VideoBitrate != nil && bitrate > 200_000_000 {
			return fail("VideoBitrate", "The exact video bitrate is outside the supported encoder range.")
		}
	}
	output.Index, output.Codec, output.IsDefault, output.IsAVC, output.IsAVCKnown = 0, "h264", true, true, true
	output.CodecTag, output.CodecTagString, output.TimeBase = "avc1", "avc1", ""
	output.Language, output.Title, output.AudioTiming = "", "", nil
	return bitrate, nil
}

func progressiveVideoDimensions(width, height, maxWidth, maxHeight int, exactWidth, exactHeight *int) (int, int, bool) {
	if exactWidth != nil && exactHeight != nil {
		width, height = *exactWidth, *exactHeight
	} else if exactWidth != nil {
		height = max(2, int(int64(height)*int64(*exactWidth)/int64(width))/2*2)
		width = *exactWidth
	} else if exactHeight != nil {
		width = max(2, int(int64(width)*int64(*exactHeight)/int64(height))/2*2)
		height = *exactHeight
	} else {
		width, height = conversionDimensions(width, height, maxWidth, maxHeight)
	}
	return width, height, width >= 2 && height >= 2 && width <= maxWidth && height <= maxHeight && width <= 8192 && height <= 8192 && width%2 == 0 && height%2 == 0
}

func progressiveVideoExactFrameRate(value float64) bool {
	if value < 1 || value > 240 || math.IsNaN(value) || math.IsInf(value, 0) {
		return false
	}
	text := strconv.FormatFloat(value, 'f', -1, 64)
	if position := strings.IndexByte(text, '.'); position >= 0 && len(text)-position-1 > 6 {
		return false
	}
	return true
}

type progressiveVideoAudio struct {
	present        bool
	codec          string
	bitrate, cost  int64
	channels, rate int
	output         media.Stream
}

func progressiveVideoAudioTracks(source media.Stream, sourceBitrate int64, request ProgressiveVideoRequest, limits ConversionLimits, copy bool, budget int64) []progressiveVideoAudio {
	channelLimit := limits.MaxAudioChannels
	if request.MaxAudioChannels != nil {
		channelLimit = min(channelLimit, *request.MaxAudioChannels)
	}
	rateLimit := math.MaxInt32
	if request.MaxSampleRate != nil {
		rateLimit = *request.MaxSampleRate
	}
	if request.MaxAudioBitrate != nil {
		budget = min(budget, *request.MaxAudioBitrate)
	}
	if copy {
		cost := source.Bitrate
		if cost <= 0 {
			cost = sourceBitrate
		}
		if !strings.EqualFold(source.Codec, "aac") || source.Channels > channelLimit || source.Channels > 8 || source.SampleRate > rateLimit || cost <= 0 || cost > budget ||
			request.AudioChannels != nil && source.Channels != *request.AudioChannels || request.AudioSampleRate != nil && source.SampleRate != *request.AudioSampleRate ||
			request.AudioBitrate != nil && (source.Bitrate <= 0 || source.Bitrate != *request.AudioBitrate) {
			return nil
		}
		source.Index, source.IsDefault, source.Codec = 1, true, "aac"
		source.CodecTag, source.CodecTagString, source.TimeBase = "mp4a", "mp4a", ""
		source.Language, source.Title, source.AudioTiming = "", "", nil
		return []progressiveVideoAudio{{present: true, codec: "copy", cost: cost, output: source}}
	}
	channels := min(source.Channels, channelLimit)
	if request.AudioChannels != nil {
		channels = *request.AudioChannels
	}
	if channels < 1 || channels > channelLimit || channels > 8 {
		return nil
	}
	var candidates []progressiveVideoAudio
	for _, rate := range progressiveAudioRates("aac", source.SampleRate, rateLimit, request.AudioSampleRate) {
		bitrate, ok := progressiveAudioTarget("aac", rate, channels, budget, request.AudioBitrate)
		if !ok {
			continue
		}
		output := media.Stream{Index: 1, Codec: "aac", CodecType: "audio", Profile: "LC", CodecTag: "mp4a", CodecTagString: "mp4a",
			Channels: channels, SampleRate: rate, Bitrate: bitrate, IsDefault: true}
		candidates = append(candidates, progressiveVideoAudio{present: true, codec: "aac", bitrate: bitrate, cost: bitrate, channels: channels, rate: rate, output: output})
	}
	return candidates
}
