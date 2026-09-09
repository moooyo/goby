package playback

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

// ConversionLimits carries already authorized server and user limits. Zero
// numeric limits use the initial server defaults; permissions default to false.
// Hardware is a requested execution policy, never evidence of available devices.
type ConversionLimits struct {
	MaxBitrate                                           int64
	MaxWidth, MaxHeight, MaxAudioChannels                int
	AllowRemux, AllowAudioTranscode, AllowVideoTranscode bool
	Hardware                                             transcode.Hardware
}

// ConversionDecision keeps original-file evaluation separate from the projected
// HLS output. A nil Plan is an ordinary unsupported decision, not a request error.
// Method is DirectStream for remuxing and Transcode when a stream is encoded.
// Output contains the selected external subtitle representation for the HLS
// container. No decision here performs authorization or starts an encoder.
type ConversionDecision struct {
	Original     Decision
	Output       Decision
	OutputSource Source
	Plan         *transcode.Plan
	Method       string
	Reasons      []Reason
}

// PlanConversion chooses a bounded MPEG-TS HLS conversion that the supplied
// client profile accepts. It leaves Evaluate's original-file behavior intact.
// The supported output codecs are H.264, AAC and MP3. Embedded subtitles,
// deinterlacing and HDR tone mapping need separate implementations.
func PlanConversion(source Source, request Request, limits ConversionLimits) (ConversionDecision, error) {
	original, err := Evaluate(source, request)
	if err != nil {
		return ConversionDecision{}, err
	}
	result := ConversionDecision{Original: original}
	decline := func(code, property, message string) (ConversionDecision, error) {
		result.Reasons = append(result.Reasons, Reason{Code: code, Property: property, Message: message})
		return result, nil
	}
	limits, err = normalizeConversionLimits(limits)
	if err != nil {
		return result, err
	}
	if request.DeviceProfile == nil {
		return decline("conversion_profile_required", "DeviceProfile", "A declared HLS transcoding profile is required to establish output compatibility.")
	}
	if err := validateConversionProfiles(request.DeviceProfile.TranscodingProfiles); err != nil {
		return result, err
	}
	request = conversionRequestAliases(request)
	selection, err := selectStreams(source, request)
	if err != nil {
		return result, err
	}
	kind := sourceKind(source, selection)
	if kind == "" || request.LiveStreamID != "" {
		return decline("conversion_source_unsupported", "Type", "Only indexed local audio and video sources can be converted.")
	}
	if source.Info.DurationTicks <= 0 || request.StartTimeTicks != nil && *request.StartTimeTicks >= source.Info.DurationTicks {
		return decline("conversion_duration_required", "StartTimeTicks", "HLS conversion requires a positive remaining source duration.")
	}
	if selection.audio != nil && selection.audio.IsExternal || selection.video != nil && selection.video.IsExternal {
		return decline("conversion_external_stream_unsupported", "AudioStreamIndex", "Audio and video conversion inputs must belong to the opened original file.")
	}
	if selection.subtitle != nil && (!selection.subtitle.IsExternal || !selection.subtitle.IsTextSubtitleStream) {
		return decline("conversion_subtitle_unsupported", "SubtitleStreamIndex", "HLS conversion currently supports separately delivered indexed external text subtitles only.")
	}
	var failures []Reason
	for _, profile := range request.DeviceProfile.TranscodingProfiles {
		if !conversionProfileMatches(profile, kind) {
			continue
		}
		if reason := unsupportedConversionOptions(profile); reason != nil {
			failures = appendConversionReasons(failures, *reason)
			continue
		}
		// Lower-cost candidates are tested first. Every candidate is independently
		// projected and checked; a source mismatch cannot authorize the output.
		modes := [][2]bool{{true, true}, {true, false}, {false, true}, {false, false}}
		if kind == DlnaProfileTypeAudio {
			modes = [][2]bool{{true, true}, {true, false}}
		}
		for _, mode := range modes {
			videoCopy, audioCopy := mode[0], mode[1]
			if selection.video == nil || kind == DlnaProfileTypeAudio {
				videoCopy = true
			}
			if selection.audio == nil {
				audioCopy = true
			}
			if !conversionModeAllowed(selection, kind, request, limits, videoCopy, audioCopy) {
				continue
			}
			for _, candidateProfile := range conversionAudioProfiles(profile, audioCopy) {
				plan, projected, reason := conversionCandidate(source, request, limits, candidateProfile, kind, selection, videoCopy, audioCopy)
				if reason != nil {
					failures = appendConversionReasons(failures, *reason)
					continue
				}
				outputRequest := conversionOutputRequest(request, candidateProfile, kind)
				output, evaluateErr := Evaluate(projected, outputRequest)
				if evaluateErr != nil {
					return result, evaluateErr
				}
				if !output.OriginalCompatible || !output.ProfileMatched ||
					selection.subtitle != nil && output.SubtitleMethod != SubtitleDeliveryMethodExternal {
					failures = appendConversionReasons(failures, output.Reasons...)
					continue
				}
				if err := transcode.ValidatePlan(plan); err != nil {
					failures = appendConversionReasons(failures, *conversionReason("conversion_runner_limit", "TranscodingProfiles", "The requested conversion exceeds the bounded runner's supported plan settings."))
					continue
				}
				result.Plan, result.Output, result.OutputSource = &plan, output, projected
				result.Method = "DirectStream"
				if !videoCopy || !audioCopy {
					result.Method = "Transcode"
				}
				result.Reasons = output.Reasons
				return result, nil
			}
		}
	}
	result.Reasons = failures
	return decline("no_supported_hls_conversion", "TranscodingProfiles", "No authorized and constructible HLS output satisfies the client profile and requested limits.")
}

func normalizeConversionLimits(limits ConversionLimits) (ConversionLimits, error) {
	if limits.MaxBitrate < 0 || limits.MaxBitrate > 1_000_000_000 || limits.MaxWidth < 0 || limits.MaxWidth > 8192 ||
		limits.MaxHeight < 0 || limits.MaxHeight > 8192 || limits.MaxAudioChannels < 0 || limits.MaxAudioChannels > 8 {
		return limits, fmt.Errorf("%w: invalid server conversion limits", ErrInvalidRequest)
	}
	if limits.MaxBitrate == 0 {
		limits.MaxBitrate = 20_000_000
	}
	if limits.MaxWidth == 0 {
		limits.MaxWidth = 1920
	}
	if limits.MaxHeight == 0 {
		limits.MaxHeight = 1080
	}
	if limits.MaxAudioChannels == 0 {
		limits.MaxAudioChannels = 8
	}
	return limits, nil
}

func validateConversionProfiles(profiles []TranscodingProfile) error {
	for _, profile := range profiles {
		if len(profile.Container) > maxProfileText || len(profile.VideoCodec) > maxProfileText || len(profile.AudioCodec) > maxProfileText ||
			len(profile.Protocol) > maxProfileText || len(profile.Context) > maxProfileText || len(profile.Type) > maxProfileText ||
			len(profile.MaxAudioChannels) > 32 || len(profile.ManifestSubtitles) > maxProfileText ||
			profile.MaxWidth != nil && (*profile.MaxWidth < 1 || *profile.MaxWidth > 65536) ||
			profile.MaxHeight != nil && (*profile.MaxHeight < 1 || *profile.MaxHeight > 65536) ||
			profile.SegmentLength != nil && *profile.SegmentLength < 1 || profile.MinSegments != nil && *profile.MinSegments < 0 {
			return fmt.Errorf("%w: invalid transcoding profile settings", ErrInvalidRequest)
		}
		if profile.MaxAudioChannels != "" {
			channels, err := strconv.Atoi(profile.MaxAudioChannels)
			if err != nil || channels < 1 || channels > 256 {
				return fmt.Errorf("%w: invalid transcoding channel limit", ErrInvalidRequest)
			}
		}
	}
	return nil
}

func conversionProfileMatches(profile TranscodingProfile, kind DlnaProfileType) bool {
	return strings.EqualFold(string(profile.Type), string(kind)) && strings.EqualFold(profile.Protocol, "hls") &&
		(profile.Context == "" || strings.EqualFold(string(profile.Context), string(EncodingContextStreaming))) &&
		(matchesList(profile.Container, "ts") || matchesList(profile.Container, "mpegts")) &&
		(kind != DlnaProfileTypeVideo || matchesList(profile.VideoCodec, "h264"))
}

func conversionRequestAliases(request Request) Request {
	profile := *request.DeviceProfile
	profile.TranscodingProfiles = append([]TranscodingProfile(nil), profile.TranscodingProfiles...)
	for index := range profile.TranscodingProfiles {
		profile.TranscodingProfiles[index].Container = conversionContainerSelector(profile.TranscodingProfiles[index].Container)
	}
	profile.ContainerProfiles = append([]ContainerProfile(nil), profile.ContainerProfiles...)
	for index := range profile.ContainerProfiles {
		profile.ContainerProfiles[index].Container = conversionContainerSelector(profile.ContainerProfiles[index].Container)
	}
	profile.CodecProfiles = append([]CodecProfile(nil), profile.CodecProfiles...)
	for index := range profile.CodecProfiles {
		profile.CodecProfiles[index].Container = conversionContainerSelector(profile.CodecProfiles[index].Container)
	}
	profile.SubtitleProfiles = append([]SubtitleProfile(nil), profile.SubtitleProfiles...)
	for index := range profile.SubtitleProfiles {
		profile.SubtitleProfiles[index].Container = conversionContainerSelector(profile.SubtitleProfiles[index].Container)
	}
	request.DeviceProfile = &profile
	return request
}

func conversionContainerSelector(selector string) string {
	values := strings.Split(selector, ",")
	for index, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), "mpegts") {
			values[index] = "ts"
		}
	}
	return strings.Join(values, ",")
}

func conversionAudioProfiles(profile TranscodingProfile, copy bool) []TranscodingProfile {
	if copy {
		return []TranscodingProfile{profile}
	}
	var result []TranscodingProfile
	for _, codec := range []string{"aac", "mp3"} {
		if matchesList(profile.AudioCodec, codec) {
			candidate := profile
			candidate.AudioCodec = codec
			result = append(result, candidate)
		}
	}
	return result
}

func unsupportedConversionOptions(profile TranscodingProfile) *Reason {
	if isTrue(profile.EnableMpegtsM2TsMode) || isTrue(profile.CopyTimestamps) || isTrue(profile.BreakOnNonKeyFrames) ||
		profile.SegmentLength != nil && *profile.SegmentLength > 10 || profile.MinSegments != nil && *profile.MinSegments > 1 ||
		profile.ManifestSubtitles != "" || profile.MaxManifestSubtitles != nil && *profile.MaxManifestSubtitles > 0 ||
		profile.TranscodeSeekInfo == TranscodeSeekInfoBytes {
		return conversionReason("conversion_profile_option_unsupported", "TranscodingProfiles", "The HLS profile requires output settings not implemented by the bounded conversion runner.")
	}
	return nil
}

func conversionModeAllowed(selection selectedStreams, kind DlnaProfileType, request Request, limits ConversionLimits, videoCopy, audioCopy bool) bool {
	if kind == DlnaProfileTypeVideo && selection.video != nil && (videoCopy && isFalse(request.AllowVideoStreamCopy) || !videoCopy && !limits.AllowVideoTranscode) {
		return false
	}
	if selection.audio != nil && (audioCopy && isFalse(request.AllowAudioStreamCopy) || !audioCopy && !limits.AllowAudioTranscode) {
		return false
	}
	if videoCopy && audioCopy {
		return limits.AllowRemux && !isFalse(request.EnableDirectStream)
	}
	return !isFalse(request.EnableTranscoding)
}

type conversionCaps struct {
	width, height, channels, sampleRate int
	frameRate                           float64
	videoBitrate, audioBitrate          int64
}

func conversionCandidate(source Source, request Request, limits ConversionLimits, profile TranscodingProfile, kind DlnaProfileType, selection selectedStreams, videoCopy, audioCopy bool) (transcode.Plan, Source, *Reason) {
	plan := transcode.Plan{Container: "ts", VideoStreamIndex: -1, AudioStreamIndex: -1,
		DurationTicks: source.Info.DurationTicks, SegmentSeconds: 6}
	if request.StartTimeTicks != nil {
		plan.StartTicks = *request.StartTimeTicks
	}
	if profile.SegmentLength != nil {
		plan.SegmentSeconds = *profile.SegmentLength
	}
	projected := Source{ItemID: source.ItemID, MediaSourceID: source.MediaSourceID, ItemType: source.ItemType, Path: "output.ts",
		Info: media.Info{Container: "mpegts", DurationTicks: source.Info.DurationTicks}}
	fail := func(code, property, message string) (transcode.Plan, Source, *Reason) {
		return plan, projected, conversionReason(code, property, message)
	}
	caps := conversionCaps{width: limits.MaxWidth, height: limits.MaxHeight, channels: limits.MaxAudioChannels,
		sampleRate: 48_000, frameRate: 60, videoBitrate: limits.MaxBitrate, audioBitrate: 512_000}
	if profile.MaxWidth != nil {
		caps.width = min(caps.width, *profile.MaxWidth)
	}
	if profile.MaxHeight != nil {
		caps.height = min(caps.height, *profile.MaxHeight)
	}
	if profile.MaxAudioChannels != "" {
		channels, _ := strconv.Atoi(profile.MaxAudioChannels)
		caps.channels = min(caps.channels, channels)
	}
	if request.MaxAudioChannels != nil {
		caps.channels = min(caps.channels, *request.MaxAudioChannels)
	}
	totalLimit := limits.MaxBitrate
	for _, limit := range []*int64{request.MaxStreamingBitrate, request.DeviceProfile.MaxStreamingBitrate} {
		if limit != nil {
			totalLimit = min(totalLimit, *limit)
		}
	}
	if kind == DlnaProfileTypeAudio && request.DeviceProfile.MusicStreamingTranscodingBitrate != nil {
		if *request.DeviceProfile.MusicStreamingTranscodingBitrate < 1 {
			return fail("conversion_audio_bitrate_invalid", "MusicStreamingTranscodingBitrate", "The music streaming bitrate must be positive.")
		}
		caps.audioBitrate = min(caps.audioBitrate, int64(*request.DeviceProfile.MusicStreamingTranscodingBitrate))
	}
	var video, audio *media.Stream
	if selection.video != nil && kind == DlnaProfileTypeVideo {
		stream := *selection.video
		video = &stream
		plan.VideoStreamIndex = video.Index
		if video.Width < 1 || video.Height < 1 || video.Width > 65536 || video.Height > 65536 {
			return fail("conversion_video_dimensions_unknown", "Width", "Known bounded source dimensions are required for video conversion.")
		}
		if video.IsInterlaced || conversionHDR(video) {
			return fail("conversion_video_range_unsupported", "VideoRange", "This HLS conversion path does not implement interlaced output or HDR tone mapping.")
		}
		if videoCopy {
			if !video.InterlaceKnown && (isFalse(request.AllowInterlacedVideoStreamCopy) || isFalse(profile.AllowInterlacedVideoStreamCopy)) {
				return fail("conversion_interlace_unverified", "AllowInterlacedVideoStreamCopy", "Copied video must be known progressive when interlaced stream copy is disabled.")
			}
			if !strings.EqualFold(video.Codec, "h264") || !matchesList(profile.VideoCodec, video.Codec) {
				return fail("conversion_video_copy_codec_unsupported", "VideoCodec", "The selected video cannot be copied into the declared H.264 HLS output.")
			}
			plan.VideoCodec = "copy"
			if video.Bitrate <= 0 {
				video.Bitrate = source.Info.Bitrate
			}
		} else {
			plan.VideoCodec = "h264"
			plan.Hardware = limits.Hardware
			video.Codec, video.BitDepth, video.PixelFormat = "h264", 8, "yuv420p"
			video.Profile, video.Level, video.RefFrames = "", 0, 0
			video.ColorRange, video.ColorSpace, video.ColorTransfer, video.ColorPrimaries = "", "", "", ""
			video.IsInterlaced, video.InterlaceKnown, video.FieldOrder = false, true, "progressive"
		}
		// MPEG-TS carries H.264 Annex B. MP4's AVC framing and codec tags must
		// not survive the output projection even when the payload is copied.
		video.IsAVC, video.IsAVCKnown, video.CodecTag, video.CodecTagString = false, true, "", ""
		video.TimeBase = ""
		video.IsDefault = true
		projected.Info.Streams = append(projected.Info.Streams, *video)
	}
	if selection.audio != nil {
		stream := *selection.audio
		audio = &stream
		plan.AudioStreamIndex = audio.Index
		if audio.Channels < 1 || audio.Channels > 256 {
			return fail("conversion_audio_channels_unknown", "AudioChannels", "Known source audio channels are required for conversion.")
		}
		if audioCopy {
			if (!strings.EqualFold(audio.Codec, "aac") && !strings.EqualFold(audio.Codec, "mp3")) || !matchesList(profile.AudioCodec, audio.Codec) {
				return fail("conversion_audio_copy_codec_unsupported", "AudioCodec", "The selected audio cannot be copied into the declared AAC or MP3 HLS output.")
			}
			plan.AudioCodec = "copy"
			if audio.Bitrate <= 0 {
				audio.Bitrate = source.Info.Bitrate
			}
		} else {
			codec := ""
			for _, candidate := range []string{"aac", "mp3"} {
				if matchesList(profile.AudioCodec, candidate) {
					codec = candidate
					break
				}
			}
			if codec == "" {
				return fail("conversion_audio_codec_unsupported", "AudioCodec", "The declared profile offers no supported audio encoder.")
			}
			plan.AudioCodec, audio.Codec, audio.Profile, audio.BitDepth = codec, codec, "", 0
			if codec == "aac" {
				audio.Profile = "LC"
			} else {
				caps.channels, caps.audioBitrate = min(caps.channels, 2), min(caps.audioBitrate, 320_000)
			}
		}
		audio.CodecTag, audio.CodecTagString, audio.IsDefault = "", "", true
		audio.TimeBase = ""
		projected.Info.Streams = append(projected.Info.Streams, *audio)
	}
	if selection.subtitle != nil {
		projected.Info.Streams = append(projected.Info.Streams, *selection.subtitle)
	}
	// Conditions are interpreted against the candidate's output codec and
	// container. Restrictive numeric conditions can reduce encoder targets;
	// Evaluate below still checks the complete predicate after projection.
	for iteration := 0; iteration < 8; iteration++ {
		outputSelection, _ := selectStreams(projected, request)
		applyConversionCaps(&caps, request.DeviceProfile, kind, conditionFacts{source: projected, streams: outputSelection})
		if video != nil && (caps.width < 2 || caps.height < 2 || caps.frameRate <= 0) ||
			audio != nil && (caps.channels < 1 || caps.sampleRate < 8_000) {
			return fail("conversion_output_limits_unsupported", "CodecProfiles", "The requested output limits cannot be constructed by this conversion path.")
		}
		if audio != nil {
			if audioCopy {
				if audio.Channels > caps.channels || audio.SampleRate <= 0 || audio.SampleRate > caps.sampleRate || audio.Bitrate <= 0 || audio.Bitrate > caps.audioBitrate {
					return fail("conversion_audio_copy_limit", "AudioCodec", "Copied audio exceeds an output limit or lacks facts needed to verify it.")
				}
			} else {
				audio.Channels = min(audio.Channels, caps.channels)
				audio.SampleRate = conversionSampleRate(caps.sampleRate, audio.Codec)
				if audio.SampleRate == 0 {
					return fail("conversion_audio_sample_rate_unsupported", "AudioSampleRate", "The requested sample rate is not supported by the selected encoder.")
				}
				audioBudget := totalLimit * 9 / 10
				if video != nil {
					reserved := int64(128_000)
					if videoCopy {
						reserved = video.Bitrate
					}
					audioBudget -= reserved
				}
				audio.Bitrate = min(int64(audio.Channels)*96_000, caps.audioBitrate, audioBudget)
				if audio.Codec == "mp3" {
					audio.Bitrate = conversionMP3Bitrate(audio.Bitrate)
				}
				if audio.Bitrate < 32_000 {
					return fail("conversion_audio_bitrate_too_low", "AudioBitrate", "The audio bitrate budget is below the supported encoder range.")
				}
				audio.ChannelLayout = ""
			}
			if !audioCopy {
				plan.AudioBitrate, plan.AudioChannels, plan.AudioSampleRate = audio.Bitrate, audio.Channels, audio.SampleRate
			}
		}
		if video != nil {
			if videoCopy {
				if video.Width > caps.width || video.Height > caps.height || video.Bitrate <= 0 || video.Bitrate > caps.videoBitrate || conversionFrameRate(video) > caps.frameRate {
					return fail("conversion_video_copy_limit", "VideoCodec", "Copied video exceeds an output limit or lacks bitrate facts needed to verify it.")
				}
			} else {
				video.Width, video.Height = conversionDimensions(video.Width, video.Height, caps.width, caps.height)
				fps := conversionFrameRate(video)
				if fps <= 0 {
					return fail("conversion_frame_rate_unknown", "VideoFramerate", "A known source frame rate is required for encoded video.")
				}
				fps = math.Floor(min(fps, caps.frameRate)*1_000_000) / 1_000_000
				video.AverageFrameRate, video.RealFrameRate = strconv.FormatFloat(fps, 'f', 6, 64), ""
				budget := totalLimit * 9 / 10
				if audio != nil {
					budget -= audio.Bitrate
				}
				if budget < 128_000 {
					return fail("conversion_video_bitrate_too_low", "VideoBitrate", "The remaining video bitrate budget is below the supported encoder range.")
				}
				bitrate := video.Bitrate
				if bitrate <= 0 {
					bitrate = 4_000_000
				}
				bitrate = max(bitrate, 128_000)
				video.Bitrate = min(bitrate, caps.videoBitrate, budget)
				if video.Bitrate < 128_000 {
					return fail("conversion_video_bitrate_too_low", "VideoBitrate", "The video bitrate constraint is below the supported encoder range.")
				}
			}
			if !videoCopy {
				plan.Width, plan.Height, plan.FrameRate, plan.VideoBitrate = video.Width, video.Height, conversionFrameRate(video), video.Bitrate
			}
		}
		var payloadBitrate int64
		if video != nil {
			payloadBitrate += video.Bitrate
		}
		if audio != nil {
			payloadBitrate += audio.Bitrate
		}
		// Reserve ten percent for transport overhead. This is an advertised media
		// planning budget, not a claim of packet-level peak bandwidth policing.
		if payloadBitrate <= 0 || payloadBitrate > totalLimit*9/10 {
			return fail("conversion_total_bitrate_limit", "MaxStreamingBitrate", "The selected stream bitrates exceed the HLS output budget including transport headroom.")
		}
		projected.Info.Bitrate = (payloadBitrate*10 + 8) / 9
		for index := range projected.Info.Streams {
			if video != nil && projected.Info.Streams[index].Index == video.Index {
				projected.Info.Streams[index] = *video
			}
			if audio != nil && projected.Info.Streams[index].Index == audio.Index {
				projected.Info.Streams[index] = *audio
			}
		}
		// Applicability can change after downmixing or resizing. Resolve the
		// monotonically decreasing caps against the resulting output facts before
		// allowing the caller to perform the final complete profile evaluation.
		nextCaps := caps
		nextSelection, _ := selectStreams(projected, request)
		applyConversionCaps(&nextCaps, request.DeviceProfile, kind, conditionFacts{source: projected, streams: nextSelection})
		if nextCaps == caps {
			return plan, projected, nil
		}
		caps = nextCaps
	}
	return fail("conversion_conditions_did_not_converge", "CodecProfiles", "Output conditions require more refinement than the bounded planner supports.")
}

func conversionOutputRequest(request Request, profile TranscodingProfile, kind DlnaProfileType) Request {
	output := request
	device := *request.DeviceProfile
	device.DirectPlayProfiles = []DirectPlayProfile{{Type: kind, Container: "ts", VideoCodec: profile.VideoCodec, AudioCodec: profile.AudioCodec}}
	device.MaxStaticMusicBitrate = nil
	output.DeviceProfile = &device
	enabled := true
	output.EnableDirectPlay, output.EnableDirectStream, output.EnableTranscoding = &enabled, &enabled, &enabled
	return output
}

func applyConversionCaps(caps *conversionCaps, profile *DeviceProfile, kind DlnaProfileType, facts conditionFacts) {
	apply := func(conditions []ProfileCondition) {
		for _, condition := range conditions {
			if !strings.EqualFold(string(condition.Condition), string(ProfileConditionTypeLessThanEqual)) &&
				!strings.EqualFold(string(condition.Condition), string(ProfileConditionTypeEquals)) {
				continue
			}
			value, ok := boundedNumber(condition.Value)
			if !ok || value.Sign() <= 0 {
				continue
			}
			number, _ := value.Float64()
			if math.IsNaN(number) || math.IsInf(number, 0) || number > 1_000_000_000 {
				continue
			}
			switch strings.ToLower(string(condition.Property)) {
			case "width":
				caps.width = min(caps.width, int(number))
			case "height":
				caps.height = min(caps.height, int(number))
			case "audiochannels":
				caps.channels = min(caps.channels, int(number))
			case "audiosamplerate":
				caps.sampleRate = min(caps.sampleRate, int(number))
			case "videoframerate":
				caps.frameRate = min(caps.frameRate, number)
			case "videobitrate":
				caps.videoBitrate = min(caps.videoBitrate, int64(number))
			case "audiobitrate":
				caps.audioBitrate = min(caps.audioBitrate, int64(number))
			}
		}
	}
	for _, constraint := range profile.ContainerProfiles {
		if strings.EqualFold(string(constraint.Type), string(kind)) && matchesList(constraint.Container, "ts") {
			apply(constraint.Conditions)
		}
	}
	for _, constraint := range profile.CodecProfiles {
		stream := codecProfileStream(constraint.Type, kind, facts.streams)
		if stream == nil || !matchesList(constraint.Container, "ts") || !matchesList(constraint.Codec, stream.Codec) {
			continue
		}
		if applicable, _ := applies(constraint.ApplyConditions, facts); applicable {
			apply(constraint.Conditions)
		}
	}
}

func conversionHDR(video *media.Stream) bool {
	if video.VideoRangeKnown && !strings.EqualFold(video.VideoRange, "SDR") {
		return true
	}
	switch strings.ToLower(video.ColorTransfer) {
	case "smpte2084", "arib-std-b67":
		return true
	}
	// Untagged high-bit-depth video cannot establish an SDR conversion without
	// richer HDR side-data probing. Ordinary 8-bit sources are not assigned a
	// fabricated VideoRange fact; required unknown range conditions still fail.
	format := strings.ToLower(video.PixelFormat)
	highDepth := video.BitDepth > 8 || strings.HasPrefix(format, "p010") || strings.HasPrefix(format, "p016")
	for _, suffix := range []string{"p9", "p10", "p12", "p14", "p16"} {
		highDepth = highDepth || strings.Contains(format, suffix)
	}
	return highDepth && !video.VideoRangeKnown &&
		!strings.EqualFold(video.ColorTransfer, "bt709") && !strings.EqualFold(video.ColorTransfer, "smpte170m") && !strings.EqualFold(video.ColorTransfer, "iec61966-2-1")
}

func conversionFrameRate(video *media.Stream) float64 {
	for _, text := range []string{video.AverageFrameRate, video.RealFrameRate} {
		value, ok := boundedNumber(text)
		if !ok || value.Sign() <= 0 {
			continue
		}
		fps, _ := value.Float64()
		if fps > 0 && fps <= 1000 {
			return fps
		}
	}
	return 0
}

func conversionDimensions(width, height, maxWidth, maxHeight int) (int, int) {
	scale := min(1.0, float64(maxWidth)/float64(width), float64(maxHeight)/float64(height))
	return max(2, int(float64(width)*scale)/2*2), max(2, int(float64(height)*scale)/2*2)
}

func conversionSampleRate(maximum int, codec string) int {
	rates := []int{48_000, 44_100, 32_000, 24_000, 22_050, 16_000, 12_000, 11_025, 8_000}
	if codec == "mp3" {
		rates = rates[:3]
	}
	for _, rate := range rates {
		if rate <= maximum {
			return rate
		}
	}
	return 0
}

func conversionMP3Bitrate(maximum int64) int64 {
	for _, bitrate := range []int64{320_000, 256_000, 224_000, 192_000, 160_000, 128_000, 112_000, 96_000, 80_000, 64_000, 56_000, 48_000, 40_000, 32_000} {
		if bitrate <= maximum {
			return bitrate
		}
	}
	return 0
}

func conversionReason(code, property, message string) *Reason {
	return &Reason{Code: code, Property: property, Message: message}
}

func appendConversionReasons(current []Reason, incoming ...Reason) []Reason {
	for _, reason := range incoming {
		if len(current) >= 32 {
			break
		}
		duplicate := false
		for _, existing := range current {
			if existing.Code == reason.Code && existing.Property == reason.Property {
				duplicate = true
				break
			}
		}
		if !duplicate {
			current = append(current, reason)
		}
	}
	return current
}

func isTrue(value *bool) bool  { return value != nil && *value }
func isFalse(value *bool) bool { return value != nil && !*value }
