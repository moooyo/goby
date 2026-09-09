package playback

import (
	"iter"
	"math"
	"math/big"
	"reflect"
	"strconv"
	"strings"
)

const (
	maxVideoProfileVariants = 64
	maxVideoProfileStates   = 8
	maxVideoProfileAttempts = 2048
	maxVideoProfileNodes    = maxVideoProfileAttempts * 8
)

type videoProfileBudget struct {
	audioProfileBudget
	nodes int
}

func (budget *videoProfileBudget) visit() bool {
	if budget.nodes == 0 {
		budget.exhausted = true
		return false
	}
	budget.nodes--
	return true
}

// PlanVideoConversion preserves the client's video profile order across HTTP
// and HLS. The reference establishes an omitted protocol/context as
// HTTP/Streaming. Original remains the complete original-file evaluation;
// client capabilities never replace independently authorized conversion limits.
func PlanVideoConversion(source Source, request Request, limits ConversionLimits) (ConversionDecision, error) {
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
	if sourceKind(source, selection) != DlnaProfileTypeVideo || request.LiveStreamID != "" || selection.video == nil ||
		selection.video.IsExternal || selection.audio != nil && selection.audio.IsExternal {
		result.Reasons = []Reason{*conversionReason("video_profile_source_unsupported", "Type", "Video profile conversion requires an internal local video source.")}
		return result, nil
	}
	if request.DeviceProfile == nil {
		result.Reasons = []Reason{*conversionReason("video_profile_required", "DeviceProfile", "Video conversion negotiation requires declared client profiles.")}
		return result, nil
	}
	search := videoProfileBudget{audioProfileBudget: audioProfileBudget{remaining: maxVideoProfileAttempts}, nodes: maxVideoProfileNodes}
	searchLimit := func() (ConversionDecision, error) {
		if len(result.Reasons) >= 32 {
			result.Reasons = result.Reasons[:31]
		}
		result.Reasons = appendConversionReasons(result.Reasons, *conversionReason("video_profile_search_limit", "TranscodingProfiles", "Video profile negotiation exhausted its bounded candidate search without a verified output."))
		return result, nil
	}
	for index, candidate := range request.DeviceProfile.TranscodingProfiles {
		if !strings.EqualFold(string(candidate.Type), string(DlnaProfileTypeVideo)) {
			continue
		}
		if err := validateConversionProfiles([]TranscodingProfile{candidate}); err != nil {
			result.Reasons = appendConversionReasons(result.Reasons, *conversionReason("invalid_video_transcoding_profile", "TranscodingProfiles", "A video output profile contains malformed settings."))
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
			// A single video HLS profile evaluates its original source and up
			// to four video/audio copy modes with two encoded audio choices.
			if !search.take(9) {
				return searchLimit()
			}
			selected, err = PlanConversion(source, single, limits)
		case "", "http":
			protocol = "http"
			selected = planHTTPVideoProfile(source, single, candidate, limits, selection, &search)
		default:
			selected.Reasons = []Reason{*conversionReason("video_profile_protocol_unsupported", "Protocol", "The declared video delivery protocol is not implemented.")}
		}
		if err != nil {
			result.Reasons = appendConversionReasons(result.Reasons, *conversionReason("video_profile_unconstructible", "TranscodingProfiles", "The video output profile could not be evaluated as a constructible conversion."))
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
		if protocol == "http" {
			attachProgressiveVideoSeekCandidate(source, selected.Plan)
		}
		return selected, nil
	}
	result.Reasons = appendConversionReasons(result.Reasons, *conversionReason("no_supported_video_profile", "TranscodingProfiles", "No declared video profile has an authorized output satisfying the client constraints."))
	return result, nil
}

func planHTTPVideoProfile(source Source, request Request, profile TranscodingProfile, limits ConversionLimits, selection selectedStreams, search *videoProfileBudget) ConversionDecision {
	var result ConversionDecision
	var unverified *ConversionDecision
	// Restart evidence cannot affect profile compatibility. Select it once for
	// the final accepted plan, rather than revalidating a large scan index for
	// every bounded profile-search attempt. This value copy leaves Source intact.
	source.Info.VideoSeekIndexes = nil
	if selection.subtitle != nil || !httpVideoProfileOptionsSupported(profile) {
		result.Reasons = []Reason{*conversionReason("video_profile_options_unsupported", "TranscodingProfiles", "The progressive profile requires delivery behavior not represented by the video execution plan.")}
		return result
	}
	if !videoProfileHasSelector(profile.Container, "mp4") || !videoProfileHasSelector(profile.VideoCodec, "h264") ||
		selection.audio != nil && !videoProfileHasSelector(profile.AudioCodec, "aac") {
		result.Reasons = []Reason{*conversionReason("progressive_video_format_unsupported", "TranscodingProfiles", "Progressive video requires an explicit MP4/H.264 profile and AAC support when the source contains audio.")}
		return result
	}
	if isFalse(request.EnableTranscoding) {
		limits.AllowVideoTranscode, limits.AllowAudioTranscode = false, false
		if isFalse(request.EnableDirectStream) {
			limits.AllowRemux = false
		}
	}
	videoIndex := selection.video.Index
	base := ProgressiveVideoRequest{OutputContainer: "mp4", VideoCodec: "h264", AudioCodec: "none", VideoStreamIndex: &videoIndex,
		MaxBitrate: lowerLimit(request.MaxStreamingBitrate, request.DeviceProfile.MaxStreamingBitrate), MaxAudioChannels: request.MaxAudioChannels,
		MaxWidth: profile.MaxWidth, MaxHeight: profile.MaxHeight, AllowVideoStreamCopy: request.AllowVideoStreamCopy,
		AllowAudioStreamCopy: request.AllowAudioStreamCopy, AllowInterlacedVideoStreamCopy: request.AllowInterlacedVideoStreamCopy}
	if selection.audio != nil {
		index := selection.audio.Index
		base.AudioCodec, base.AudioStreamIndex = "aac", &index
	}
	if isFalse(profile.AllowInterlacedVideoStreamCopy) {
		disabled := false
		base.AllowInterlacedVideoStreamCopy = &disabled
	}
	if request.StartTimeTicks != nil {
		base.StartTimeTicks = *request.StartTimeTicks
	}
	if profile.MaxAudioChannels != "" {
		channels, _ := strconv.Atoi(profile.MaxAudioChannels)
		if base.MaxAudioChannels == nil || channels < *base.MaxAudioChannels {
			base.MaxAudioChannels = &channels
		}
	}
	// Copy-first candidates preserve verified source facts. Explicit encoding
	// retries can establish controlled facts that were unknown in copied media.
	for _, force := range [][2]bool{{false, false}, {false, true}, {true, false}, {true, true}} {
		attempt := base
		if force[0] {
			if !limits.AllowVideoTranscode || isFalse(base.AllowVideoStreamCopy) {
				continue
			}
			disabled := false
			attempt.AllowVideoStreamCopy = &disabled
		}
		if force[1] {
			if selection.audio == nil || !limits.AllowAudioTranscode || isFalse(base.AllowAudioStreamCopy) {
				continue
			}
			disabled := false
			attempt.AllowAudioStreamCopy = &disabled
		}
		states := [][]ProfileCondition{videoOutputConditions(request.DeviceProfile, selection.audio != nil, nil)}
		for state := 0; state < len(states) && state < maxVideoProfileStates; state++ {
			for variant := range videoProfileRequests(attempt, selection, limits, states[state], search) {
				if !search.take(1) {
					return result
				}
				converted, err := PlanProgressiveVideo(source, variant, limits)
				if err != nil || converted.Plan == nil {
					result.Reasons = appendConversionReasons(result.Reasons, converted.Reasons...)
					continue
				}
				outputRequest := videoOutputRequest(request, selection.audio != nil)
				output, err := Evaluate(converted.OutputSource, outputRequest)
				if err != nil {
					result.Reasons = appendConversionReasons(result.Reasons, *conversionReason("video_output_evaluation_failed", "TranscodingProfiles", "Projected MP4 output could not be evaluated against the client profile."))
					continue
				}
				if output.OriginalCompatible && output.ProfileMatched {
					candidate := ConversionDecision{Plan: converted.Plan, OutputSource: converted.OutputSource, Output: output, Method: converted.Method, Reasons: output.Reasons}
					if !output.ClientMustValidate {
						return candidate
					}
					// Optional unknown facts remain permissible, but a later
					// candidate in the same profile may verify them completely.
					if unverified == nil {
						unverified = &candidate
					}
				}
				result.Reasons = appendConversionReasons(result.Reasons, output.Reasons...)
				streams, err := selectStreams(converted.OutputSource, outputRequest)
				if err != nil {
					continue
				}
				facts := conditionFacts{source: converted.OutputSource, streams: streams}
				next := videoOutputConditions(request.DeviceProfile, selection.audio != nil, &facts)
				seen := false
				for _, previous := range states {
					seen = seen || reflect.DeepEqual(previous, next)
				}
				if !seen && len(states) < maxVideoProfileStates {
					states = append(states, next)
				}
			}
			if search.exhausted {
				return result
			}
		}
	}
	if unverified != nil {
		return *unverified
	}
	result.Reasons = appendConversionReasons(result.Reasons, *conversionReason("progressive_video_profile_unavailable", "TranscodingProfiles", "The progressive video profile has no constructible output satisfying its conditions."))
	return result
}

func httpVideoProfileOptionsSupported(profile TranscodingProfile) bool {
	return httpAudioProfileOptionsSupported(profile) &&
		(profile.MinSegments == nil || *profile.MinSegments == 0) && profile.SegmentLength == nil
}

func videoProfileHasSelector(selector, expected string) bool {
	for _, value := range audioProfileSelectors(selector) {
		if value == expected {
			return true
		}
	}
	return false
}

func videoOutputRequest(request Request, hasAudio bool) Request {
	output := request
	device := *request.DeviceProfile
	device.DirectPlayProfiles = []DirectPlayProfile{{Type: DlnaProfileTypeVideo, Container: "mp4", VideoCodec: "h264", AudioCodec: "aac"}}
	output.DeviceProfile = &device
	noSubtitle, enabled := -1, true
	output.AudioStreamIndex, output.SubtitleStreamIndex, output.StartTimeTicks = nil, &noSubtitle, nil
	if hasAudio {
		index := 1
		output.AudioStreamIndex = &index
	}
	output.EnableDirectPlay, output.EnableDirectStream, output.EnableTranscoding = &enabled, &enabled, &enabled
	return output
}

func videoOutputConditions(profile *DeviceProfile, hasAudio bool, facts *conditionFacts) []ProfileCondition {
	var conditions []ProfileCondition
	for _, constraint := range profile.ContainerProfiles {
		if strings.EqualFold(string(constraint.Type), string(DlnaProfileTypeVideo)) && matchesList(constraint.Container, "mp4") {
			conditions = append(conditions, constraint.Conditions...)
		}
	}
	for _, constraint := range profile.CodecProfiles {
		codec := ""
		switch {
		case strings.EqualFold(string(constraint.Type), string(CodecTypeVideo)):
			codec = "h264"
		case hasAudio && strings.EqualFold(string(constraint.Type), string(CodecTypeVideoAudio)):
			codec = "aac"
		}
		if codec == "" || !matchesList(constraint.Container, "mp4") || !matchesList(constraint.Codec, codec) {
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

func videoProfileRequests(base ProgressiveVideoRequest, streams selectedStreams, limits ConversionLimits, conditions []ProfileCondition, search *videoProfileBudget) iter.Seq[ProgressiveVideoRequest] {
	return func(yield func(ProgressiveVideoRequest) bool) {
		// Copied video can satisfy numeric conditions without replacing its
		// timing or geometry. No complete Cartesian product is materialized.
		if !search.visit() || !yield(base) {
			return
		}
		var properties []ProfileConditionValue
		var predicates [][]videoNumericCondition
		for _, property := range []ProfileConditionValue{ProfileConditionValueWidth, ProfileConditionValueHeight, ProfileConditionValueVideoFramerate,
			ProfileConditionValueAudioChannels, ProfileConditionValueAudioSampleRate, ProfileConditionValueAudioBitrate, ProfileConditionValueVideoBitrate} {
			if !audioHasConditions(conditions, property) || streams.audio == nil &&
				(property == ProfileConditionValueAudioChannels || property == ProfileConditionValueAudioSampleRate || property == ProfileConditionValueAudioBitrate) {
				continue
			}
			compiled, valid := compileVideoNumericConditions(conditions, property, search)
			if !valid {
				return
			}
			properties, predicates = append(properties, property), append(predicates, compiled)
		}
		var visit func(int, ProgressiveVideoRequest) bool
		visit = func(index int, current ProgressiveVideoRequest) bool {
			if !search.visit() {
				return false
			}
			if index == len(properties) {
				return reflect.DeepEqual(current, base) || yield(current)
			}
			property := properties[index]
			values := videoProfileNumberDomain(property, current, streams, limits, predicates[index], search)
			if search.exhausted {
				return false
			}
			for _, value := range values {
				candidate, integer, bitrate, fraction := current, int(value), int64(value), value
				switch property {
				case ProfileConditionValueWidth:
					candidate.Width = &integer
				case ProfileConditionValueHeight:
					candidate.Height = &integer
				case ProfileConditionValueVideoFramerate:
					candidate.FrameRate = &fraction
				case ProfileConditionValueAudioChannels:
					candidate.AudioChannels = &integer
				case ProfileConditionValueAudioSampleRate:
					candidate.AudioSampleRate = &integer
				case ProfileConditionValueAudioBitrate:
					candidate.AudioBitrate = &bitrate
				case ProfileConditionValueVideoBitrate:
					candidate.VideoBitrate = &bitrate
				}
				if !visit(index+1, candidate) {
					return false
				}
			}
			return true
		}
		visit(0, base)
	}
}

type videoNumericCondition struct {
	operator string
	values   []*big.Rat
}

func compileVideoNumericConditions(conditions []ProfileCondition, property ProfileConditionValue, search *videoProfileBudget) ([]videoNumericCondition, bool) {
	var result []videoNumericCondition
	for _, condition := range conditions {
		if !strings.EqualFold(string(condition.Property), string(property)) {
			continue
		}
		compiled := videoNumericCondition{operator: strings.ToLower(string(condition.Condition))}
		switch compiled.operator {
		case "equals", "notequals", "lessthanequal", "greaterthanequal", "equalsany":
		default:
			return nil, false
		}
		texts := []string{strings.TrimSpace(condition.Value)}
		if compiled.operator == "equalsany" {
			texts = strings.FieldsFunc(condition.Value, func(value rune) bool { return value == ',' || value == '|' })
		}
		if len(texts) == 0 || len(texts) > maxProfileEntries {
			return nil, false
		}
		for _, text := range texts {
			if !search.visit() {
				return nil, false
			}
			value, valid := boundedNumber(strings.TrimSpace(text))
			if !valid {
				return nil, false
			}
			compiled.values = append(compiled.values, value)
		}
		result = append(result, compiled)
	}
	return result, true
}

func videoProfileNumberDomain(property ProfileConditionValue, request ProgressiveVideoRequest, streams selectedStreams, limits ConversionLimits, conditions []videoNumericCondition, search *videoProfileBudget) []float64 {
	var values []float64
	seen := make(map[float64]struct{})
	minimum, maximum := float64(1), float64(limits.MaxBitrate)
	if request.MaxBitrate != nil {
		maximum = min(maximum, float64(*request.MaxBitrate))
	}
	add := func(value float64) {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < minimum || value > maximum || search.exhausted || len(values) == maxVideoProfileVariants {
			return
		}
		if (property == ProfileConditionValueWidth || property == ProfileConditionValueHeight) && int(value)%2 != 0 {
			return
		}
		if _, exists := seen[value]; exists {
			return
		}
		if !search.visit() {
			return
		}
		seen[value] = struct{}{}
		// Intersect all unary predicates before applying the domain bound.
		// Cache rejected values too; repeated input never reparses predicates.
		if !videoNumericConditionsMatch(conditions, value) {
			return
		}
		values = append(values, value)
	}
	switch property {
	case ProfileConditionValueWidth, ProfileConditionValueHeight:
		minimum, maximum = 2, float64(limits.MaxWidth)
		if property == ProfileConditionValueHeight {
			maximum = float64(limits.MaxHeight)
			if request.MaxHeight != nil {
				maximum = min(maximum, float64(*request.MaxHeight))
			}
		} else if request.MaxWidth != nil {
			maximum = min(maximum, float64(*request.MaxWidth))
		}
		widthCap, heightCap := limits.MaxWidth, limits.MaxHeight
		if request.MaxWidth != nil {
			widthCap = min(widthCap, *request.MaxWidth)
		}
		if request.MaxHeight != nil {
			heightCap = min(heightCap, *request.MaxHeight)
		}
		width, height := conversionDimensions(streams.video.Width, streams.video.Height, widthCap, heightCap)
		if property == ProfileConditionValueWidth {
			if request.Height != nil && streams.video.Height > 0 {
				add(math.Floor(float64(streams.video.Width)*float64(*request.Height)/float64(streams.video.Height)/2) * 2)
			}
			add(float64(width))
			add(float64(streams.video.Width))
		} else {
			if request.Width != nil && streams.video.Width > 0 {
				add(math.Floor(float64(streams.video.Height)*float64(*request.Width)/float64(streams.video.Width)/2) * 2)
			}
			add(float64(height))
			add(float64(streams.video.Height))
		}
	case ProfileConditionValueVideoFramerate:
		minimum, maximum = 1, 240
		if request.MaxFrameRate != nil {
			maximum = min(maximum, *request.MaxFrameRate)
		}
		fps := conversionFrameRate(streams.video)
		add(fps)
		add(math.Floor(fps*1_000_000) / 1_000_000)
	case ProfileConditionValueAudioChannels:
		maximum = float64(limits.MaxAudioChannels)
		if request.MaxAudioChannels != nil {
			maximum = min(maximum, float64(*request.MaxAudioChannels))
		}
		add(min(float64(streams.audio.Channels), maximum))
		for value := maximum; value >= minimum; value-- {
			add(value)
		}
		return values
	case ProfileConditionValueAudioSampleRate:
		maximum = 96_000
		if request.MaxSampleRate != nil {
			maximum = min(maximum, float64(*request.MaxSampleRate))
		}
		add(float64(streams.audio.SampleRate))
		for _, value := range progressiveAudioRates("aac", streams.audio.SampleRate, int(maximum), nil) {
			add(float64(value))
		}
		return values
	case ProfileConditionValueVideoBitrate:
		minimum = 64_000
		add(float64(streams.video.Bitrate))
		add(min(4_000_000, maximum))
	case ProfileConditionValueAudioBitrate:
		minimum, maximum = 8_000, min(768_000, maximum)
		add(float64(streams.audio.Bitrate))
		channels := min(streams.audio.Channels, limits.MaxAudioChannels)
		if request.MaxAudioChannels != nil {
			channels = min(channels, *request.MaxAudioChannels)
		}
		if request.AudioChannels != nil {
			channels = *request.AudioChannels
		}
		add(min(float64(channels*96_000), maximum))
	}
	for _, condition := range conditions {
		for _, number := range condition.values {
			if search.exhausted || len(values) == maxVideoProfileVariants {
				return values
			}
			if number.Sign() <= 0 {
				continue
			}
			value, _ := number.Float64()
			if math.IsNaN(value) || math.IsInf(value, 0) || value > maximum+2 || value < minimum-2 {
				continue
			}
			if property == ProfileConditionValueVideoFramerate {
				add(value)
				for _, units := range []float64{math.Floor(value * 1_000_000), math.Ceil(value * 1_000_000)} {
					add(units / 1_000_000)
					add((units - 1) / 1_000_000)
					add((units + 1) / 1_000_000)
				}
			}
			for _, candidate := range []float64{math.Floor(value), math.Ceil(value), math.Floor(value) - 1, math.Ceil(value) + 1} {
				add(candidate)
			}
			if property == ProfileConditionValueWidth || property == ProfileConditionValueHeight {
				for _, units := range []float64{math.Floor(value / 2), math.Ceil(value / 2)} {
					add(units * 2)
					add((units - 1) * 2)
					add((units + 1) * 2)
				}
			}
		}
	}
	add(maximum)
	if property == ProfileConditionValueWidth || property == ProfileConditionValueHeight {
		add(math.Floor(maximum/2) * 2)
	}
	add(minimum)
	return values
}

func videoNumericConditionsMatch(conditions []videoNumericCondition, value float64) bool {
	actual, valid := boundedNumber(strconv.FormatFloat(value, 'f', -1, 64))
	if !valid {
		return false
	}
	for _, condition := range conditions {
		matched := false
		for _, expected := range condition.values {
			comparison := actual.Cmp(expected)
			switch condition.operator {
			case "equals", "equalsany":
				matched = comparison == 0
			case "notequals":
				matched = comparison != 0
			case "lessthanequal":
				matched = comparison <= 0
			case "greaterthanequal":
				matched = comparison >= 0
			}
			if matched {
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}
