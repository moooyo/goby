package playback

import (
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

// ProgressiveAudioRequest contains normalized media requirements, not HTTP
// defaults. The caller must select one output container and one codec. Exact
// targets remain separate from ceilings. AudioBitrate is an encoder target;
// MaxBitrate is a media planning budget, not instantaneous network policing.
type ProgressiveAudioRequest struct {
	OutputContainer, AudioCodec string
	AudioStreamIndex            *int
	StartTimeTicks              int64
	AudioBitrate                *int64
	AudioChannels               *int
	AudioSampleRate             *int
	AudioBitDepth               *int
	MaxBitrate                  *int64
	MaxAudioChannels            *int
	MaxSampleRate               *int
	AllowAudioStreamCopy        *bool
}

// ProgressiveAudioDecision describes one immutable conversion. Plan uses the
// original input stream index; OutputSource contains only output audio index 0.
// OutputSource.Info.Bitrate is a media bitrate planning value: a copied source
// declaration, PCM payload rate, lossy encoder target, or FLAC frame bound. It
// excludes container overhead and is not a measured result or HTTP bandwidth
// guarantee. The stream's Bitrate remains zero for content-dependent FLAC.
// DurationTicks is the requested output window, not a measured audio boundary;
// the caller must supply a trustworthy source duration before planning.
type ProgressiveAudioDecision struct {
	Plan         *transcode.Plan
	OutputSource Source
	Method       string
	Reasons      []Reason
}

const (
	progressiveWAVHeaderBytes   = int64(68)
	progressiveFLACBlockSamples = int64(4096)
	progressiveMaxDuration      = int64(30*24*60*60) * media.TicksPerSecond
)

// PlanProgressiveAudio evaluates explicit progressive output requirements. It
// performs no authorization, I/O, session lookup, or original-file delivery.
// Permissions are supplied by the caller; unsupported valid requirements return
// a nil Plan and reasons. Structural errors wrap ErrInvalidRequest or Source.
func PlanProgressiveAudio(source Source, request ProgressiveAudioRequest, limits ConversionLimits) (ProgressiveAudioDecision, error) {
	var result ProgressiveAudioDecision
	if err := validateProgressiveAudioRequest(request); err != nil {
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
	limits, err = normalizeConversionLimits(limits)
	if err != nil {
		return result, err
	}
	decline := func(code, property, message string) (ProgressiveAudioDecision, error) {
		result.Reasons = appendConversionReasons(result.Reasons, Reason{Code: code, Property: property, Message: message})
		return result, nil
	}
	if sourceKind(source, selection) != DlnaProfileTypeAudio || selection.audio == nil || selection.audio.IsExternal {
		return decline("progressive_audio_source_unsupported", "Type", "Progressive audio requires an indexed local audio source and an internal audio track.")
	}
	if source.Info.DurationTicks <= 0 || source.Info.DurationTicks > progressiveMaxDuration || request.StartTimeTicks >= source.Info.DurationTicks {
		return decline("progressive_audio_duration_unsupported", "StartTimeTicks", "The requested audio output must have a positive bounded remaining duration.")
	}
	audio := *selection.audio
	if audio.Bitrate < 0 || audio.BitDepth < 0 {
		return result, fmt.Errorf("%w: negative audio stream facts", ErrInvalidSource)
	}
	if audio.Channels <= 0 || audio.Channels > 256 || audio.SampleRate <= 0 || audio.SampleRate > math.MaxInt32 {
		return decline("progressive_audio_facts_missing", "AudioStreamIndex", "Known source channel count and sample rate are required to plan audio conversion.")
	}
	var sourceSamples int64
	if timing := audio.AudioTiming; timing != nil && timing.Exact {
		duration, known := progressiveCeilProduct(timing.SampleCount, media.TicksPerSecond, int64(audio.SampleRate))
		if !source.Info.AudioDurationExact || !known || timing.SampleCount <= 0 || timing.PacketCount <= 0 ||
			timing.StartTicks != 0 || timing.EndTicks != source.Info.DurationTicks || duration != source.Info.DurationTicks {
			return decline("progressive_audio_timing_inconsistent", "AudioTiming", "Exact audio samples must describe the complete selected presentation from time zero.")
		}
		sourceSamples = timing.SampleCount
	}
	container, codec := strings.ToLower(request.OutputContainer), strings.ToLower(request.AudioCodec)
	audio.Codec = strings.ToLower(audio.Codec)
	if codec != "copy" && !progressiveAudioCodecFits(container, codec) || codec == "copy" && !progressiveAudioCodecFits(container, audio.Codec) {
		return decline("progressive_audio_format_unsupported", "OutputContainer", "The selected output container and codec combination is not implemented.")
	}
	budget := limits.MaxBitrate
	if request.MaxBitrate != nil {
		budget = min(budget, *request.MaxBitrate)
	}
	channelLimit := limits.MaxAudioChannels
	if request.MaxAudioChannels != nil {
		channelLimit = min(channelLimit, *request.MaxAudioChannels)
	}
	rateLimit := math.MaxInt32
	if request.MaxSampleRate != nil {
		rateLimit = *request.MaxSampleRate
	}
	if request.AudioChannels != nil && *request.AudioChannels > channelLimit || request.AudioSampleRate != nil && *request.AudioSampleRate > rateLimit {
		return decline("progressive_audio_conflicting_limits", "AudioChannels", "An exact audio target exceeds a requested or authorized output ceiling.")
	}
	if request.AudioBitDepth != nil && codec != "copy" && codec != "flac" && (codec != "pcm_s16le" || *request.AudioBitDepth != 16) {
		return decline("progressive_audio_depth_unsupported", "AudioBitDepth", "The selected audio codec cannot guarantee the requested integer bit depth.")
	}
	remaining := source.Info.DurationTicks - request.StartTimeTicks
	if !isFalse(request.AllowAudioStreamCopy) && limits.AllowRemux && (codec == "copy" || codec == audio.Codec) {
		plan := progressiveAudioPlan(source, request, container, "copy", audio.Index, audio.SampleRate, sourceSamples)
		outputSamples, samplesErr := transcode.ProgressiveOutputSamples(plan, audio.SampleRate)
		copyBitrate, copyDepth := audio.Bitrate, audio.BitDepth
		if audio.Codec == "pcm_s16le" {
			copyBitrate, copyDepth = int64(audio.SampleRate)*int64(audio.Channels)*16, 16
		}
		copyAllowed := progressiveAudioCodecFits(container, audio.Codec) && audio.Channels <= channelLimit &&
			samplesErr == nil && audio.Channels <= progressiveAudioMaxChannels(container, audio.Codec) && audio.SampleRate <= rateLimit &&
			(request.AudioChannels == nil || *request.AudioChannels == audio.Channels) &&
			(request.AudioSampleRate == nil || *request.AudioSampleRate == audio.SampleRate) &&
			(request.AudioBitDepth == nil || copyDepth > 0 && *request.AudioBitDepth == copyDepth) &&
			(request.AudioBitrate == nil || copyBitrate > 0 && *request.AudioBitrate == copyBitrate)
		if container == "aac" && audio.Channels > 6 || audio.Codec == "flac" && request.StartTimeTicks != 0 ||
			audio.Codec == "opus" && audio.SampleRate != 48_000 {
			copyAllowed = false
		}
		// Ogg page seeks can retain a valid sample count at the wrong source
		// position. A nonzero conversion start needs decoded sample trimming;
		// an explicit copy cannot silently become an inaccurate packet seek.
		if media.CanonicalContainer(source.Info, source.Path) == "ogg" && request.StartTimeTicks != 0 {
			copyAllowed = false
		}
		// AAC in other containers may use ASC features that ADTS cannot carry.
		// Codec/profile names alone do not establish framing compatibility.
		if container == "aac" && media.CanonicalContainer(source.Info, source.Path) != "aac" {
			copyAllowed = false
		}
		// Native PCM WAV carries the format facts needed to construct an exact
		// replacement header. PCM packets from other containers need encoding.
		if container == "wav" && (media.CanonicalContainer(source.Info, source.Path) != "wav" || source.Info.Size <= 0 ||
			audio.Index != 0 || audio.BitDepth > 0 && audio.BitDepth != 16) {
			copyAllowed = false
		}
		if copyAllowed && container == "wav" {
			dataBytes, sizeOK := progressiveCeilProduct(outputSamples, 2*int64(audio.Channels), 1)
			if !sizeOK || dataBytes > math.MaxUint32-progressiveWAVHeaderBytes {
				copyAllowed = false
			}
		}
		if copyAllowed {
			planned, known := progressiveCopyBitrate(source, audio)
			if known && planned <= budget {
				if transcode.ValidatePlan(plan) == nil {
					result.Plan, result.Method = &plan, "DirectStream"
					result.OutputSource = progressiveAudioOutput(source, audio, container, remaining, planned)
					return result, nil
				}
			}
		}
	}
	if codec == "copy" {
		return decline("progressive_audio_copy_unavailable", "AllowAudioStreamCopy", "The requested copy cannot satisfy source facts, output limits, or remux permission.")
	}
	if !limits.AllowAudioTranscode {
		return decline("progressive_audio_encoding_denied", "AudioCodec", "Audio encoding is not permitted by the current conversion policy.")
	}
	channels := min(audio.Channels, channelLimit, progressiveAudioMaxChannels(container, codec))
	if request.AudioChannels != nil {
		channels = *request.AudioChannels
	}
	if channels < 1 || channels > progressiveAudioMaxChannels(container, codec) {
		return decline("progressive_audio_channels_unsupported", "AudioChannels", "The selected encoder and container cannot construct the requested channel count.")
	}
	rates := progressiveAudioRates(codec, audio.SampleRate, rateLimit, request.AudioSampleRate)
	if len(rates) == 0 {
		return decline("progressive_audio_rate_unsupported", "AudioSampleRate", "The selected encoder cannot construct a sample rate satisfying the exact target and ceiling.")
	}
	for _, rate := range rates {
		plan := progressiveAudioPlan(source, request, container, codec, audio.Index, audio.SampleRate, sourceSamples)
		plan.AudioChannels, plan.AudioSampleRate = channels, rate
		outputSamples, samplesErr := transcode.ProgressiveOutputSamples(plan, rate)
		if samplesErr != nil {
			result.Reasons = appendConversionReasons(result.Reasons, *conversionReason("progressive_audio_sample_window_invalid", "StartTimeTicks", "The selected audio window contains no valid bounded output samples."))
			continue
		}
		output := media.Stream{Index: 0, Codec: codec, CodecType: "audio", Channels: channels, SampleRate: rate, IsDefault: true}
		var planned int64
		var valid bool
		switch codec {
		case "flac":
			if request.AudioBitrate != nil {
				result.Reasons = appendConversionReasons(result.Reasons, *conversionReason("progressive_flac_bitrate_uncontrolled", "AudioBitrate", "FLAC does not implement an exact compressed-bitrate target."))
				continue
			}
			depth := 24
			if audio.BitDepth > 0 && audio.BitDepth <= 16 {
				depth = 16
			}
			if request.AudioBitDepth != nil {
				depth = *request.AudioBitDepth
			} else if audio.BitDepth > 24 {
				result.Reasons = appendConversionReasons(result.Reasons, *conversionReason("progressive_flac_precision_unsupported", "AudioBitDepth", "The source precision exceeds supported FLAC output; an explicit lower bit depth is required."))
				continue
			}
			if depth != 16 && depth != 24 {
				continue
			}
			plan.AudioBitDepth, output.BitDepth = depth, depth
			planned, valid = progressiveLosslessBudget(codec, channels, rate, depth, outputSamples)
		case "pcm_s16le":
			output.BitDepth, output.Bitrate = 16, int64(rate)*int64(channels)*16
			if request.AudioBitrate != nil && *request.AudioBitrate != output.Bitrate {
				continue
			}
			dataBytes, sizeOK := progressiveCeilProduct(outputSamples, 2*int64(channels), 1)
			if !sizeOK || dataBytes > math.MaxUint32-progressiveWAVHeaderBytes {
				continue
			}
			planned, valid = output.Bitrate, true
		default:
			bitrate, ok := progressiveAudioTarget(codec, rate, channels, budget, request.AudioBitrate)
			if !ok {
				continue
			}
			plan.AudioBitrate, output.Bitrate = bitrate, bitrate
			if codec == "aac" {
				output.Profile = "LC"
			}
			planned, valid = bitrate, true
		}
		if !valid || planned > budget {
			continue
		}
		if transcode.ValidatePlan(plan) != nil {
			result.Reasons = appendConversionReasons(result.Reasons, *conversionReason("progressive_audio_runner_limit", "AudioCodec", "The planned output exceeds the execution engine's supported settings."))
			continue
		}
		result.Plan, result.Method = &plan, "Transcode"
		result.OutputSource = progressiveAudioOutput(source, output, container, remaining, planned)
		result.Reasons = nil
		return result, nil
	}
	return decline("progressive_audio_no_output", "AudioBitrate", "No constructible audio output satisfies the exact targets and authorized planning budget.")
}

func validateProgressiveAudioRequest(request ProgressiveAudioRequest) error {
	for _, value := range []string{request.OutputContainer, request.AudioCodec} {
		if len(value) == 0 || len(value) > 32 {
			return fmt.Errorf("%w: a single explicit output container and codec are required", ErrInvalidRequest)
		}
		for _, character := range value {
			if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
				character >= '0' && character <= '9' || character == '_' || character == '-') {
				return fmt.Errorf("%w: malformed progressive audio selector", ErrInvalidRequest)
			}
		}
	}
	if request.StartTimeTicks < 0 || request.AudioStreamIndex != nil && (*request.AudioStreamIndex < 0 || *request.AudioStreamIndex > math.MaxInt32) {
		return fmt.Errorf("%w: invalid progressive audio position or stream index", ErrInvalidRequest)
	}
	for _, value := range []*int{request.AudioChannels, request.AudioSampleRate, request.AudioBitDepth, request.MaxAudioChannels, request.MaxSampleRate} {
		if value != nil && (*value <= 0 || *value > math.MaxInt32) {
			return fmt.Errorf("%w: audio targets and ceilings must be positive int32 values", ErrInvalidRequest)
		}
	}
	if request.AudioBitrate != nil && *request.AudioBitrate <= 0 || request.MaxBitrate != nil && *request.MaxBitrate <= 0 {
		return fmt.Errorf("%w: audio bitrate targets and ceilings must be positive", ErrInvalidRequest)
	}
	return nil
}

func progressiveAudioCodecFits(container, codec string) bool {
	return transcode.ProgressiveContainerSupportsCodec(container, codec)
}

func progressiveAudioMaxChannels(container, codec string) int {
	if codec == "mp3" {
		return 2
	}
	if container == "aac" {
		return 6
	}
	return 8
}

func progressiveAudioRates(codec string, sourceRate, limit int, exact *int) []int {
	// This is the runner's closed supported set, not every rate accepted by each
	// upstream encoder. Prefer preserving the source rate before other choices.
	allowed := []int{8000, 11025, 12000, 16000, 22050, 24000, 32000, 44100, 48000, 64000, 88200, 96000}
	switch codec {
	case "mp3", "vorbis":
		allowed = allowed[:9]
	case "opus":
		allowed = []int{48000}
	}
	var rates []int
	if exact != nil {
		for _, rate := range allowed {
			if rate == *exact && rate <= limit {
				return []int{rate}
			}
		}
		return nil
	}
	for _, rate := range allowed {
		if rate == sourceRate && rate <= limit {
			rates = append(rates, rate)
		}
	}
	for index := len(allowed) - 1; index >= 0; index-- {
		if rate := allowed[index]; rate < sourceRate && rate <= limit {
			rates = append(rates, rate)
		}
	}
	for _, rate := range allowed {
		if rate > sourceRate && rate <= limit {
			rates = append(rates, rate)
		}
	}
	return rates
}

func progressiveAudioTarget(codec string, rate, channels int, ceiling int64, exact *int64) (int64, bool) {
	minimum, maximum := int64(8_000), int64(768_000)
	var mp3Rates []int64
	switch codec {
	case "mp3":
		mp3Rates = progressiveMP3Rates(rate)
		minimum, maximum = mp3Rates[0], mp3Rates[len(mp3Rates)-1]
	case "aac":
		minimum = int64(channels) * 8_000
		maximum = min(maximum, 6*int64(rate)*int64(channels))
	case "opus":
		maximum = min(maximum, int64(channels)*256_000)
	case "vorbis":
		perChannelMinimum, perChannelMaximum := progressiveVorbisRates(rate)
		minimum, maximum = perChannelMinimum*int64(channels), min(maximum, perChannelMaximum*int64(channels))
		if minimum == 0 {
			return 0, false
		}
	default:
		return 0, false
	}
	maximum = min(maximum, ceiling)
	if exact != nil {
		if *exact < minimum || *exact > maximum {
			return 0, false
		}
		if codec == "mp3" {
			for _, value := range mp3Rates {
				if value == *exact {
					return value, true
				}
			}
			return 0, false
		}
		return *exact, true
	}
	value := min(max(int64(channels)*96_000, minimum), maximum)
	if codec == "mp3" {
		for index := len(mp3Rates) - 1; index >= 0; index-- {
			if mp3Rates[index] <= value {
				return mp3Rates[index], true
			}
		}
		return 0, false
	}
	return value, value >= minimum
}

func progressiveMP3Rates(rate int) []int64 {
	// LAME uses separate MPEG-1, MPEG-2 and MPEG-2.5 CBR tables. An arbitrary
	// requested number may round upward, so exact targets must match a table.
	if rate < 16_000 {
		return []int64{8_000, 16_000, 24_000, 32_000, 40_000, 48_000, 56_000, 64_000}
	}
	if rate < 32_000 {
		return []int64{8_000, 16_000, 24_000, 32_000, 40_000, 48_000, 56_000, 64_000, 80_000, 96_000, 112_000, 128_000, 144_000, 160_000}
	}
	return []int64{32_000, 40_000, 48_000, 56_000, 64_000, 80_000, 96_000, 112_000, 128_000, 160_000, 192_000, 224_000, 256_000, 320_000}
}

func progressiveVorbisRates(rate int) (int64, int64) {
	// Conservative intersections of Xiph libvorbis 1.3.7 managed-rate templates
	// across standard 1..8-channel layouts; values here are per channel. Higher
	// sampling templates may support quality VBR without a bitrate target.
	switch rate {
	case 8000:
		return 8_000, 32_000
	case 11025, 12000:
		return 12_000, 44_000
	case 16000, 22050, 24000:
		return 16_000, 86_000
	case 32000:
		return 30_000, 190_000
	case 44100, 48000:
		return 32_000, 240_000
	}
	return 0, 0
}

func progressiveAudioPlan(source Source, request ProgressiveAudioRequest, container, codec string, streamIndex, sourceSampleRate int, sourceSamples int64) transcode.Plan {
	return transcode.Plan{OutputMode: "progressive", Container: container, AudioCodec: codec,
		VideoStreamIndex: -1, AudioStreamIndex: streamIndex, AudioSourceSampleRate: sourceSampleRate,
		AudioSourceSampleCount: sourceSamples,
		AudioSampleSeek:        codec != "copy" && sourceSamples > 0 && media.CanonicalContainer(source.Info, source.Path) == "ogg",
		StartTicks:             request.StartTimeTicks, DurationTicks: source.Info.DurationTicks}
}

func progressiveAudioOutput(source Source, stream media.Stream, container string, remaining, plannedBitrate int64) Source {
	stream.Index, stream.TimeBase, stream.CodecTag, stream.CodecTagString = 0, "", "", ""
	stream.Language, stream.Title = "", ""
	stream.IsDefault, stream.IsExternal = true, false
	// Input measurements belong to the opened source, not a planned output.
	// In particular, retaining this pointer would both fabricate output timing
	// and let callers mutate the source through the output projection.
	stream.AudioTiming = nil
	if stream.Codec == "pcm_s16le" {
		stream.BitDepth, stream.Bitrate = 16, int64(stream.SampleRate)*int64(stream.Channels)*16
	}
	return Source{ItemID: source.ItemID, MediaSourceID: source.MediaSourceID, ItemType: "Audio", Path: "output." + container,
		Info: media.Info{Container: container, DurationTicks: remaining, Bitrate: plannedBitrate, Streams: []media.Stream{stream}}}
}

func progressiveCopyBitrate(source Source, audio media.Stream) (int64, bool) {
	if audio.Codec == "pcm_s16le" {
		return int64(audio.SampleRate) * int64(audio.Channels) * 16, true
	}
	// Codec declarations such as Vorbis nominal bitrate are planning hints,
	// not bounds on actual VBR output. Never label them as measured output.
	if audio.Bitrate > 0 {
		return audio.Bitrate, true
	}
	return source.Info.Bitrate, source.Info.Bitrate > 0
}

func progressiveLosslessBudget(codec string, channels, rate, depth int, samples int64) (int64, bool) {
	if codec != "flac" || samples <= 0 {
		return 0, false
	}
	// FFmpeg flacenc falls back to verbatim coding when a compressed frame
	// exceeds this bound. Stereo permits the additional side-channel bit.
	// Use the same integer output sample count as the runner and RIFF checks,
	// including the final partial frame. This is an encoded-media frame bound,
	// not a measured response bitrate. Container startup is budgeted separately.
	bitsPerSample := int64(channels) * int64(depth)
	if channels == 2 {
		bitsPerSample = int64(depth)*2 + 1
	}
	frameBytes, ok := progressiveCeilProduct(samples, bitsPerSample, 8)
	if !ok {
		return 0, false
	}
	frames := samples / progressiveFLACBlockSamples
	if samples%progressiveFLACBlockSamples != 0 {
		frames++
	}
	headerBytes, headerOK := progressiveCeilProduct(frames, int64(18+channels*((depth+14)/8)), 1)
	if !headerOK || frameBytes > math.MaxInt64-headerBytes {
		return 0, false
	}
	return progressiveCeilProduct(frameBytes+headerBytes, 8*int64(rate), samples)
}

func progressiveCeilProduct(first, second, divisor int64) (int64, bool) {
	if first < 0 || second < 0 || divisor <= 0 {
		return 0, false
	}
	value := new(big.Int).Mul(big.NewInt(first), big.NewInt(second))
	value.Add(value, big.NewInt(divisor-1))
	value.Quo(value, big.NewInt(divisor))
	return value.Int64(), value.IsInt64()
}
