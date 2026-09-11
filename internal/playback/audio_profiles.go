package playback

import (
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/moooyo/goby/internal/media"
)

const (
	maxAudioProfileVariants = 64
	maxAudioProfileStates   = 8
	maxAudioProfileAttempts = 2048
)

type audioProfileBudget struct {
	remaining int
	exhausted bool
}

func (budget *audioProfileBudget) take(count int) bool {
	if budget.remaining < count {
		budget.exhausted = true
		return false
	}
	budget.remaining -= count
	return true
}

// PlanAudioConversion preserves the client's transcoding-profile order across
// progressive HTTP and HLS delivery. Emby 4.9.5.0 reference captures establish
// empty protocol/context as HTTP/Streaming for audio. Capability declarations
// never grant conversion permissions. Original remains the unchanged complete
// source evaluation; every returned output is evaluated independently.
func PlanAudioConversion(source Source, request Request, limits ConversionLimits) (ConversionDecision, error) {
	original, err := Evaluate(source, request)
	if err != nil {
		return ConversionDecision{}, err
	}
	result := ConversionDecision{Original: original}
	limits, err = normalizeConversionLimits(limits)
	if err != nil {
		return result, err
	}
	selection, err := selectStreams(source, request)
	if err != nil {
		return result, err
	}
	if sourceKind(source, selection) != DlnaProfileTypeAudio || request.LiveStreamID != "" || selection.audio == nil || selection.audio.IsExternal {
		result.Reasons = []Reason{*conversionReason("audio_profile_source_unsupported", "Type", "Audio profile conversion requires an internal local audio source.")}
		return result, nil
	}
	if request.DeviceProfile == nil {
		result.Reasons = []Reason{*conversionReason("audio_profile_required", "DeviceProfile", "Audio conversion negotiation requires declared client profiles.")}
		return result, nil
	}
	search := audioProfileBudget{remaining: maxAudioProfileAttempts}
	searchLimit := func() (ConversionDecision, error) {
		if len(result.Reasons) >= 32 {
			result.Reasons = result.Reasons[:31]
		}
		result.Reasons = appendConversionReasons(result.Reasons, *conversionReason("audio_profile_search_limit", "TranscodingProfiles", "Audio profile negotiation exhausted its bounded candidate search without a verified output."))
		return result, nil
	}
	for index, candidate := range request.DeviceProfile.TranscodingProfiles {
		if !strings.EqualFold(string(candidate.Type), string(DlnaProfileTypeAudio)) {
			continue
		}
		if err := validateConversionProfiles([]TranscodingProfile{candidate}); err != nil {
			result.Reasons = appendConversionReasons(result.Reasons, *conversionReason("invalid_audio_transcoding_profile", "TranscodingProfiles", "An audio output profile contains malformed settings."))
			continue
		}
		device := *request.DeviceProfile
		device.TranscodingProfiles = []TranscodingProfile{candidate}
		single := request
		single.DeviceProfile = &device
		var selected ConversionDecision
		protocol := strings.ToLower(candidate.Protocol)
		switch protocol {
		case "hls":
			// A single audio HLS profile can evaluate its original source and
			// up to three distinct copy/AAC/MP3 outputs. Charge that upper bound.
			if !search.take(4) {
				return searchLimit()
			}
			selected, err = PlanConversion(source, single, limits)
		case "", "http":
			protocol = "http"
			single = audioProfileContainerAliases(single)
			candidate = single.DeviceProfile.TranscodingProfiles[0]
			selected = planHTTPAudioProfile(source, single, candidate, limits, *selection.audio, selection.subtitle != nil, &search)
		default:
			selected.Reasons = []Reason{*conversionReason("audio_profile_protocol_unsupported", "Protocol", "The declared audio delivery protocol is not implemented.")}
		}
		if err != nil {
			result.Reasons = appendConversionReasons(result.Reasons, *conversionReason("audio_profile_unconstructible", "TranscodingProfiles", "The audio output profile could not be evaluated as a constructible conversion."))
			err = nil
			continue
		}
		if selected.Plan == nil {
			result.Reasons = appendConversionReasons(result.Reasons, selected.Reasons...)
			if search.exhausted {
				return searchLimit()
			}
			continue
		}
		selected.Original = original
		selected.SelectedProtocol, selected.SelectedProfileIndex = protocol, &index
		return selected, nil
	}
	result.Reasons = appendConversionReasons(result.Reasons, *conversionReason("no_supported_audio_profile", "TranscodingProfiles", "No declared audio profile has an authorized output satisfying the client constraints."))
	return result, nil
}

func planHTTPAudioProfile(source Source, request Request, profile TranscodingProfile, limits ConversionLimits, audio media.Stream, hasSubtitle bool, search *audioProfileBudget) ConversionDecision {
	var result ConversionDecision
	if hasSubtitle || !httpAudioProfileOptionsSupported(profile) {
		result.Reasons = []Reason{*conversionReason("audio_profile_options_unsupported", "TranscodingProfiles", "The progressive profile requires delivery behavior not represented by the audio execution plan.")}
		return result
	}
	if isFalse(request.EnableTranscoding) {
		limits.AllowAudioTranscode = false
		if isFalse(request.EnableDirectStream) {
			limits.AllowRemux = false
		}
	}
	base := ProgressiveAudioRequest{AudioStreamIndex: request.AudioStreamIndex, AllowAudioStreamCopy: request.AllowAudioStreamCopy,
		MaxBitrate: lowerLimit(request.MaxStreamingBitrate, request.DeviceProfile.MaxStreamingBitrate), MaxAudioChannels: request.MaxAudioChannels}
	if request.StartTimeTicks != nil {
		base.StartTimeTicks = *request.StartTimeTicks
	}
	if profile.MaxAudioChannels != "" {
		channels, _ := strconv.Atoi(profile.MaxAudioChannels)
		if base.MaxAudioChannels == nil || channels < *base.MaxAudioChannels {
			base.MaxAudioChannels = &channels
		}
	}
	if value := request.DeviceProfile.MusicStreamingTranscodingBitrate; value != nil {
		if *value <= 0 {
			result.Reasons = []Reason{*conversionReason("audio_profile_music_bitrate_invalid", "MusicStreamingTranscodingBitrate", "The music conversion bitrate must be positive.")}
			return result
		}
		bitrate := int64(*value)
		base.MaxBitrate = lowerLimit(base.MaxBitrate, &bitrate)
	}
	// Empty selectors have no proven progressive output default. A codec and
	// container must be chosen before calling the normalized low-level planner.
	containers, codecs := audioProfileSelectors(profile.Container), audioProfileSelectors(profile.AudioCodec)
	for _, container := range containers {
		for _, codec := range codecs {
			if !progressiveAudioCodecFits(container, codec) {
				continue
			}
			base.OutputContainer, base.AudioCodec = container, codec
			for _, forceEncoding := range []bool{false, true} {
				attempt := base
				if forceEncoding {
					if !limits.AllowAudioTranscode || isFalse(base.AllowAudioStreamCopy) {
						continue
					}
					disabled := false
					attempt.AllowAudioStreamCopy = &disabled
				}
				states := [][]ProfileCondition{audioOutputConditions(request.DeviceProfile, container, codec, nil)}
				for state := 0; state < len(states) && state < maxAudioProfileStates; state++ {
					variants := audioProfileRequests(attempt, audio, limits, states[state])
					for _, variant := range variants {
						if !search.take(1) {
							return result
						}
						converted, err := PlanProgressiveAudio(source, variant, limits)
						if err != nil || converted.Plan == nil {
							result.Reasons = appendConversionReasons(result.Reasons, converted.Reasons...)
							continue
						}
						outputRequest := audioOutputRequest(request, container, codec)
						output, err := Evaluate(converted.OutputSource, outputRequest)
						if err != nil {
							result.Reasons = appendConversionReasons(result.Reasons, *conversionReason("audio_output_evaluation_failed", "TranscodingProfiles", "Projected audio output could not be evaluated against the client profile."))
							continue
						}
						if output.OriginalCompatible && output.ProfileMatched {
							return ConversionDecision{Plan: converted.Plan, OutputSource: converted.OutputSource, Output: output, Method: converted.Method, Reasons: output.Reasons}
						}
						result.Reasons = appendConversionReasons(result.Reasons, output.Reasons...)
						streams, err := selectStreams(converted.OutputSource, outputRequest)
						if err != nil {
							continue
						}
						facts := conditionFacts{source: converted.OutputSource, streams: streams}
						next := audioOutputConditions(request.DeviceProfile, container, codec, &facts)
						seen := false
						for _, previous := range states {
							seen = seen || reflect.DeepEqual(previous, next)
						}
						if !seen && len(states) < maxAudioProfileStates {
							states = append(states, next)
						}
					}
				}
			}
		}
	}
	result.Reasons = appendConversionReasons(result.Reasons, *conversionReason("progressive_audio_profile_unavailable", "TranscodingProfiles", "The progressive audio profile has no constructible output satisfying its conditions."))
	return result
}

func httpAudioProfileOptionsSupported(profile TranscodingProfile) bool {
	return (profile.Context == "" || strings.EqualFold(string(profile.Context), string(EncodingContextStreaming))) &&
		!isTrue(profile.CopyTimestamps) && !isTrue(profile.EstimateContentLength) && !isTrue(profile.EnableMpegtsM2TsMode) &&
		!isTrue(profile.BreakOnNonKeyFrames) && !isTrue(profile.FillEmptySubtitleSegments) &&
		profile.ManifestSubtitles == "" && (profile.MaxManifestSubtitles == nil || *profile.MaxManifestSubtitles == 0) &&
		(profile.TranscodeSeekInfo == "" || strings.EqualFold(string(profile.TranscodeSeekInfo), string(TranscodeSeekInfoAuto)))
}

func audioProfileSelectors(selector string) []string {
	var result []string
	for _, value := range strings.Split(selector, ",") {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || len(value) > 32 {
			continue
		}
		seen := false
		for _, existing := range result {
			seen = seen || existing == value
		}
		if !seen {
			result = append(result, value)
		}
		if len(result) == maxProfileEntries {
			break
		}
	}
	return result
}

func audioProfileContainerAliases(request Request) Request {
	canonical := func(selector string) string {
		values := strings.Split(selector, ",")
		for index, value := range values {
			switch strings.ToLower(strings.TrimSpace(value)) {
			case "mp4", "m4b":
				values[index] = "m4a"
			case "adts":
				values[index] = "aac"
			case "wave":
				values[index] = "wav"
			case "oga":
				values[index] = "ogg"
			}
		}
		return strings.Join(values, ",")
	}
	profile := *request.DeviceProfile
	profile.TranscodingProfiles = append([]TranscodingProfile(nil), profile.TranscodingProfiles...)
	for index := range profile.TranscodingProfiles {
		profile.TranscodingProfiles[index].Container = canonical(profile.TranscodingProfiles[index].Container)
	}
	profile.ContainerProfiles = append([]ContainerProfile(nil), profile.ContainerProfiles...)
	for index := range profile.ContainerProfiles {
		profile.ContainerProfiles[index].Container = canonical(profile.ContainerProfiles[index].Container)
	}
	profile.CodecProfiles = append([]CodecProfile(nil), profile.CodecProfiles...)
	for index := range profile.CodecProfiles {
		profile.CodecProfiles[index].Container = canonical(profile.CodecProfiles[index].Container)
	}
	request.DeviceProfile = &profile
	return request
}

func audioOutputRequest(request Request, container, codec string) Request {
	output := request
	device := *request.DeviceProfile
	device.DirectPlayProfiles = []DirectPlayProfile{{Type: DlnaProfileTypeAudio, Container: container, AudioCodec: codec}}
	device.MaxStaticBitrate = nil
	device.MaxStaticMusicBitrate = nil
	output.DeviceProfile = &device
	zero, noSubtitle, enabled := 0, -1, true
	output.AudioStreamIndex, output.SubtitleStreamIndex, output.StartTimeTicks = &zero, &noSubtitle, nil
	output.EnableDirectPlay, output.EnableDirectStream, output.EnableTranscoding = &enabled, &enabled, &enabled
	return output
}

func audioOutputConditions(profile *DeviceProfile, container, codec string, facts *conditionFacts) []ProfileCondition {
	var conditions []ProfileCondition
	for _, constraint := range profile.ContainerProfiles {
		if strings.EqualFold(string(constraint.Type), string(DlnaProfileTypeAudio)) && matchesList(constraint.Container, container) {
			conditions = append(conditions, constraint.Conditions...)
		}
	}
	for _, constraint := range profile.CodecProfiles {
		if !strings.EqualFold(string(constraint.Type), string(CodecTypeAudio)) || !matchesList(constraint.Container, container) || !matchesList(constraint.Codec, codec) {
			continue
		}
		if len(constraint.ApplyConditions) != 0 {
			if facts == nil {
				continue
			}
			if apply, _ := applies(constraint.ApplyConditions, *facts); !apply {
				continue
			}
		}
		conditions = append(conditions, constraint.Conditions...)
	}
	return conditions
}

func audioProfileRequests(base ProgressiveAudioRequest, source media.Stream, limits ConversionLimits, conditions []ProfileCondition) []ProgressiveAudioRequest {
	variants := []ProgressiveAudioRequest{base}
	for _, property := range []ProfileConditionValue{ProfileConditionValueAudioChannels, ProfileConditionValueAudioSampleRate, ProfileConditionValueAudioBitDepth} {
		if !audioHasConditions(conditions, property) || property == ProfileConditionValueAudioBitDepth && base.AudioCodec != "flac" && base.AudioCodec != "pcm_s16le" {
			continue
		}
		var next []ProgressiveAudioRequest
		for _, variant := range variants {
			for _, value := range audioProfileNumberDomain(property, variant, source, limits, conditions) {
				if !audioNumberConditionsMatch(conditions, property, int64(value)) {
					continue
				}
				candidate, target := variant, value
				switch property {
				case ProfileConditionValueAudioChannels:
					candidate.AudioChannels = &target
				case ProfileConditionValueAudioSampleRate:
					candidate.AudioSampleRate = &target
				case ProfileConditionValueAudioBitDepth:
					candidate.AudioBitDepth = &target
				}
				next = append(next, candidate)
				if len(next) == maxAudioProfileVariants {
					break
				}
			}
			if len(next) == maxAudioProfileVariants {
				break
			}
		}
		variants = next
	}
	// Encoded FLAC has no controlled compressed bitrate. Its required unknown
	// bitrate conditions must fail Evaluate rather than acquire invented facts.
	if !audioHasConditions(conditions, ProfileConditionValueAudioBitrate) || base.AudioCodec == "flac" {
		return variants
	}
	var result []ProgressiveAudioRequest
	for _, variant := range variants {
		for _, value := range audioProfileBitrates(variant, source, limits, conditions) {
			if !audioNumberConditionsMatch(conditions, ProfileConditionValueAudioBitrate, value) {
				continue
			}
			candidate, target := variant, value
			candidate.AudioBitrate = &target
			result = append(result, candidate)
			if len(result) == maxAudioProfileVariants {
				return result
			}
		}
	}
	return result
}

func audioProfileNumberDomain(property ProfileConditionValue, request ProgressiveAudioRequest, source media.Stream, limits ConversionLimits, conditions []ProfileCondition) []int {
	copyAllowed := limits.AllowRemux && !isFalse(request.AllowAudioStreamCopy) && strings.EqualFold(source.Codec, request.AudioCodec)
	switch property {
	case ProfileConditionValueAudioChannels:
		maximum := min(limits.MaxAudioChannels, progressiveAudioMaxChannels(request.OutputContainer, request.AudioCodec))
		if request.MaxAudioChannels != nil {
			maximum = min(maximum, *request.MaxAudioChannels)
		}
		preferred := max(1, min(source.Channels, maximum))
		values := []int{preferred}
		for value := preferred - 1; value > 0; value-- {
			values = append(values, value)
		}
		for value := preferred + 1; value <= maximum; value++ {
			values = append(values, value)
		}
		return values
	case ProfileConditionValueAudioSampleRate:
		maximum := math.MaxInt32
		if request.MaxSampleRate != nil {
			maximum = *request.MaxSampleRate
		}
		values := progressiveAudioRates(request.AudioCodec, source.SampleRate, maximum, nil)
		if copyAllowed && source.SampleRate > 0 && source.SampleRate <= maximum && (len(values) == 0 || values[0] != source.SampleRate) {
			values = append([]int{source.SampleRate}, values...)
		}
		return values
	case ProfileConditionValueAudioBitDepth:
		if request.AudioCodec == "pcm_s16le" {
			return []int{16}
		}
		var values []int
		if copyAllowed && source.BitDepth > 0 {
			values = append(values, source.BitDepth)
		}
		if source.BitDepth > 24 && audioNumberConditionsMatch(conditions, property, int64(source.BitDepth)) {
			// Unsupported original precision is not silently reduced merely
			// because the local encoder has a narrower precision range.
			return values
		}
		preferred, alternative := 24, 16
		if source.BitDepth > 0 && source.BitDepth <= 16 {
			preferred, alternative = 16, 24
		}
		for _, value := range []int{preferred, alternative} {
			if len(values) == 0 || values[0] != value {
				values = append(values, value)
			}
		}
		if len(values) == 0 {
			return nil
		}
		return values
	}
	return nil
}

func audioProfileBitrates(request ProgressiveAudioRequest, source media.Stream, limits ConversionLimits, conditions []ProfileCondition) []int64 {
	ceiling := limits.MaxBitrate
	if request.MaxBitrate != nil {
		ceiling = min(ceiling, *request.MaxBitrate)
	}
	channels := min(source.Channels, limits.MaxAudioChannels, progressiveAudioMaxChannels(request.OutputContainer, request.AudioCodec))
	if request.MaxAudioChannels != nil {
		channels = min(channels, *request.MaxAudioChannels)
	}
	if request.AudioChannels != nil {
		channels = *request.AudioChannels
	}
	rates := progressiveAudioRates(request.AudioCodec, source.SampleRate, math.MaxInt32, request.AudioSampleRate)
	var values []int64
	add := func(value int64) {
		if value < 1 || value > ceiling {
			return
		}
		for _, existing := range values {
			if existing == value {
				return
			}
		}
		values = append(values, value)
	}
	if limits.AllowRemux && !isFalse(request.AllowAudioStreamCopy) && strings.EqualFold(source.Codec, request.AudioCodec) {
		add(source.Bitrate)
	}
	for _, rate := range rates {
		if request.AudioCodec == "pcm_s16le" {
			add(int64(rate) * int64(channels) * 16)
		} else if value, ok := progressiveAudioTarget(request.AudioCodec, rate, channels, ceiling, nil); ok {
			add(value)
		}
	}
	for _, condition := range conditions {
		if !strings.EqualFold(string(condition.Property), string(ProfileConditionValueAudioBitrate)) {
			continue
		}
		for _, text := range strings.FieldsFunc(condition.Value, func(value rune) bool { return value == ',' || value == '|' }) {
			number, ok := boundedNumber(strings.TrimSpace(text))
			if !ok || number.Sign() <= 0 {
				continue
			}
			value, _ := number.Float64()
			if math.IsInf(value, 0) || math.IsNaN(value) || value > float64(ceiling)+1 {
				continue
			}
			for _, candidate := range []int64{int64(math.Floor(value)), int64(math.Ceil(value)), int64(math.Floor(value)) - 1, int64(math.Ceil(value)) + 1} {
				add(candidate)
			}
		}
	}
	if request.AudioCodec == "mp3" {
		for _, rate := range rates {
			for _, value := range progressiveMP3Rates(rate) {
				add(value)
			}
		}
	}
	add(min(ceiling, 768_000))
	return values
}

func audioHasConditions(conditions []ProfileCondition, property ProfileConditionValue) bool {
	for _, condition := range conditions {
		if strings.EqualFold(string(condition.Property), string(property)) {
			return true
		}
	}
	return false
}

func audioNumberConditionsMatch(conditions []ProfileCondition, property ProfileConditionValue, value int64) bool {
	stream := media.Stream{Index: 0, CodecType: "audio"}
	switch property {
	case ProfileConditionValueAudioChannels:
		stream.Channels = int(value)
	case ProfileConditionValueAudioSampleRate:
		stream.SampleRate = int(value)
	case ProfileConditionValueAudioBitDepth:
		stream.BitDepth = int(value)
	case ProfileConditionValueAudioBitrate:
		stream.Bitrate = value
	}
	facts := conditionFacts{source: Source{Info: media.Info{Streams: []media.Stream{stream}}}, streams: selectedStreams{audio: &stream, audioCount: 1}}
	for _, condition := range conditions {
		if strings.EqualFold(string(condition.Property), string(property)) && evaluateCondition(condition, facts) != conditionPass {
			return false
		}
	}
	return true
}
