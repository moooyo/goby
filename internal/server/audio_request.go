package server

import (
	"errors"
	"math"
	"math/big"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
	"github.com/moooyo/goby/internal/transcode"
)

var (
	errAudioRequestInvalid     = errors.New("invalid audio request")
	errAudioRequestUnsupported = errors.New("unsupported audio request")
)

type audioRequestResult struct {
	Original         bool
	Progressive      *playback.ProgressiveAudioDecision
	HLS              *playback.ConversionDecision
	StartTicks       int64
	AudioStreamIndex int
	SourceContainer  string
}

type audioContainerCapability struct {
	container string
	codec     string
}

type audioRequestOptions struct {
	request                     playback.ProgressiveAudioRequest
	static                      *bool
	autoCopy                    *bool
	transcodingMaxAudioChannels *int
}

func audioSelector(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) == 0 || len(value) > 32 {
		return "", errAudioRequestInvalid
	}
	for _, character := range value {
		if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_' || character == '-') {
			return "", errAudioRequestInvalid
		}
	}
	return value, nil
}

func audioSelectors(raw string) ([]string, error) {
	return audioSelectorList(raw, audioSelector)
}

func audioContainerSelector(raw string) (string, error) {
	container, codec, qualified := strings.Cut(raw, "|")
	container, err := audioSelector(container)
	if err != nil {
		return "", err
	}
	if !qualified {
		return container, nil
	}
	codec, err = audioSelector(codec)
	if err != nil {
		return "", err
	}
	return container + "|" + codec, nil
}

func audioSelectorList(raw string, selector func(string) (string, error)) ([]string, error) {
	if len(raw) > 1024 {
		return nil, errAudioRequestInvalid
	}
	parts := strings.Split(raw, ",")
	if len(parts) > 32 {
		return nil, errAudioRequestInvalid
	}
	var result []string
	seen := map[string]bool{}
	for _, part := range parts {
		value, err := selector(part)
		if err != nil {
			return nil, err
		}
		if !seen[value] {
			result = append(result, value)
			seen[value] = true
		}
	}
	return result, nil
}

// Container capabilities describe original bytes independently of the encoder
// matrix. Bare codec-specific labels such as opus and mp3 retain that restriction.
func audioCapability(value string) audioContainerCapability {
	if container, codec, qualified := strings.Cut(value, "|"); qualified {
		// Universal's explicit container|codec capability names the codec inside
		// that container family. It refines the label instead of requesting an
		// encoder; repeated containers with different codecs remain distinct.
		capability := audioCapability(container)
		capability.codec = codec
		return capability
	}
	switch value {
	case "m4a", "mp4", "m4b":
		return audioContainerCapability{container: "m4a"}
	case "aac", "adts":
		return audioContainerCapability{container: "aac", codec: "aac"}
	case "wav", "wave":
		return audioContainerCapability{container: "wav"}
	case "ogg", "oga":
		return audioContainerCapability{container: "ogg"}
	case "opus":
		return audioContainerCapability{container: "ogg", codec: "opus"}
	case "mp3":
		return audioContainerCapability{container: "mp3", codec: "mp3"}
	case "mp2":
		return audioContainerCapability{container: "mp3", codec: "mp2"}
	case "mpa":
		return audioContainerCapability{container: "mp3"}
	case "webma":
		return audioContainerCapability{container: "webm"}
	case "wma":
		return audioContainerCapability{container: "asf"}
	default:
		return audioContainerCapability{container: value}
	}
}

func audioSourceContainer(source playback.Source) string {
	container := media.CanonicalContainer(source.Info, source.Path)
	if strings.EqualFold(filepath.Ext(source.Path), ".m4b") {
		for _, format := range strings.Split(source.Info.Container, ",") {
			if strings.EqualFold(strings.TrimSpace(format), "mov") || strings.EqualFold(strings.TrimSpace(format), "mp4") {
				container = "m4b"
				break
			}
		}
	}
	return audioCapability(container).container
}

func audioContainers(values map[string]string, suffix string, universal bool) ([]string, error) {
	var containers []string
	if raw, exists := values["container"]; exists && raw != "" {
		var err error
		selector := audioSelector
		if universal {
			selector = audioContainerSelector
		}
		containers, err = audioSelectorList(raw, selector)
		if err != nil || !universal && len(containers) != 1 {
			return nil, errAudioRequestInvalid
		}
	}
	if suffix != "" {
		value, err := audioSelector(strings.TrimPrefix(suffix, "."))
		if err != nil {
			return nil, err
		}
		if len(containers) != 0 && (len(containers) != 1 || audioCapability(containers[0]) != audioCapability(value)) {
			return nil, errAudioRequestInvalid
		}
		containers = []string{value}
	}
	return containers, nil
}

func audioParseOptions(values map[string]string) (audioRequestOptions, error) {
	var options audioRequestOptions
	for _, field := range []struct {
		name   string
		target **bool
	}{
		{"static", &options.static}, {"enableautostreamcopy", &options.autoCopy},
		{"allowaudiostreamcopy", &options.request.AllowAudioStreamCopy},
	} {
		value, err := hlsQueryBoolean(values, field.name)
		if err != nil {
			return options, errAudioRequestInvalid
		}
		*field.target = value
	}
	if options.autoCopy != nil && !*options.autoCopy {
		disabled := false
		options.request.AllowAudioStreamCopy = &disabled
	}
	for _, field := range []struct {
		name    string
		minimum int64
		target  **int
	}{
		{"audiostreamindex", 0, &options.request.AudioStreamIndex},
		{"audiochannels", 1, &options.request.AudioChannels},
		{"maxaudiochannels", 1, &options.request.MaxAudioChannels},
		{"transcodingmaxaudiochannels", 1, &options.transcodingMaxAudioChannels},
		{"audiosamplerate", 1, &options.request.AudioSampleRate},
		{"maxsamplerate", 1, &options.request.MaxSampleRate},
		{"audiobitdepth", 1, &options.request.AudioBitDepth},
	} {
		value, err := hlsQueryInteger(values, field.minimum, math.MaxInt32, field.name)
		if err != nil {
			return options, errAudioRequestInvalid
		}
		if value != nil {
			converted := int(*value)
			*field.target = &converted
		}
	}
	for _, field := range []struct {
		name   string
		target **int64
	}{
		{"audiobitrate", &options.request.AudioBitrate}, {"maxstreamingbitrate", &options.request.MaxBitrate},
	} {
		value, err := hlsQueryInteger(values, 1, math.MaxInt64, field.name)
		if err != nil {
			return options, errAudioRequestInvalid
		}
		*field.target = value
	}
	start, err := hlsQueryInteger(values, 0, math.MaxInt64, "starttimeticks")
	if err != nil {
		return options, errAudioRequestInvalid
	}
	if start != nil {
		options.request.StartTimeTicks = *start
	}
	return options, nil
}

func audioExplicitConversion(values map[string]string, options audioRequestOptions) bool {
	if options.static != nil && !*options.static || options.request.StartTimeTicks > 0 ||
		options.request.AllowAudioStreamCopy != nil && !*options.request.AllowAudioStreamCopy {
		return true
	}
	for _, name := range []string{"transcodingprotocol", "transcodingcontainer", "audiocodec", "audiobitrate", "audiochannels", "audiosamplerate", "audiobitdepth"} {
		if value := values[name]; value != "" {
			return true
		}
	}
	return false
}

func audioOriginalFits(source playback.Source, stream media.Stream, request playback.ProgressiveAudioRequest) bool {
	if request.MaxAudioChannels != nil && (stream.Channels <= 0 || stream.Channels > *request.MaxAudioChannels) ||
		request.MaxSampleRate != nil && (stream.SampleRate <= 0 || stream.SampleRate > *request.MaxSampleRate) {
		return false
	}
	if request.MaxBitrate != nil {
		bitrate := source.Info.Bitrate
		if source.Info.Size > 0 && source.Info.DurationTicks > 0 {
			// Include all original streams, tags and artwork when a complete-file
			// average is available. Never use one track's rate as the file rate.
			value := new(big.Int).Mul(big.NewInt(source.Info.Size), big.NewInt(8*media.TicksPerSecond))
			value.Add(value, big.NewInt(source.Info.DurationTicks-1))
			value.Quo(value, big.NewInt(source.Info.DurationTicks))
			if !value.IsInt64() {
				return false
			}
			bitrate = max(bitrate, value.Int64())
		}
		if bitrate <= 0 || bitrate > *request.MaxBitrate {
			return false
		}
	}
	return true
}

func audioContainerMatches(containers []string, sourceContainer string, stream media.Stream) bool {
	for _, container := range containers {
		capability := audioCapability(container)
		if capability.container == sourceContainer && (capability.codec == "" || strings.EqualFold(capability.codec, stream.Codec)) {
			return true
		}
	}
	return false
}

func audioUnsupportedTransforms(values map[string]string) error {
	for _, name := range []string{"copytimestamps", "breakonnonkeyframes", "enablempegtsm2tsmode", "burnsubtitles", "enabletonemapping"} {
		value, err := hlsQueryBoolean(values, name)
		if err != nil {
			return errAudioRequestInvalid
		}
		if value != nil && *value {
			return errAudioRequestUnsupported
		}
	}
	for _, name := range []string{
		"audiofilter", "videofilter", "filtercomplex", "subtitlecodec", "tonemappingalgorithm", "channelmap", "channelmapping",
		"videocodec", "videobitrate", "videostreamindex", "width", "height", "maxwidth", "maxheight", "framerate", "maxframerate",
	} {
		if values[name] != "" {
			return errAudioRequestUnsupported
		}
	}
	if raw, exists := values["subtitlestreamindex"]; exists {
		value, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || value < -1 {
			return errAudioRequestInvalid
		}
		if value != -1 {
			return errAudioRequestUnsupported
		}
	}
	if method := strings.ToLower(values["subtitlemethod"]); method != "" && method != "none" && method != "external" {
		return errAudioRequestUnsupported
	}
	return nil
}

func audioProgressiveFormat(value string) (container, defaultCodec string, err error) {
	switch value {
	case "mp3":
		return "mp3", "mp3", nil
	case "aac", "adts":
		return "aac", "aac", nil
	case "m4a", "mp4", "m4b", "fmp4":
		return "m4a", "aac", nil
	case "flac":
		return "flac", "flac", nil
	case "ogg", "oga":
		return "ogg", "vorbis", nil
	case "opus":
		return "ogg", "opus", nil
	case "wav", "wave":
		return "wav", "pcm_s16le", nil
	default:
		return "", "", errAudioRequestUnsupported
	}
}

// audioRequestDecision is pure negotiation. Identity query fields are not
// authorization, source duration accuracy is the caller's responsibility, and
// Original always means the complete original bytes with client-side seeking.
func audioRequestDecision(source playback.Source, values map[string]string, suffix string, universal bool, limits playback.ConversionLimits) (audioRequestResult, error) {
	var result audioRequestResult
	values, err := hlsQueryValues(values)
	if err != nil {
		return result, errAudioRequestInvalid
	}
	options, err := audioParseOptions(values)
	if err != nil {
		return result, err
	}
	containers, err := audioContainers(values, suffix, universal)
	if err != nil {
		return result, err
	}
	var codecs []string
	if raw := values["audiocodec"]; raw != "" {
		codecs, err = audioSelectors(raw)
		if err != nil || len(codecs) > 8 {
			return result, errAudioRequestInvalid
		}
	}
	evaluation, err := playback.Evaluate(source, playback.Request{AudioStreamIndex: options.request.AudioStreamIndex})
	if err != nil {
		return result, errAudioRequestInvalid
	}
	if !strings.EqualFold(source.ItemType, "Audio") || evaluation.DefaultAudioStreamIndex == nil {
		return result, errAudioRequestUnsupported
	}
	var selected, defaultAudio *media.Stream
	for index := range source.Info.Streams {
		stream := &source.Info.Streams[index]
		if strings.EqualFold(stream.CodecType, "audio") {
			if stream.Index == *evaluation.DefaultAudioStreamIndex {
				selected = stream
			}
			if defaultAudio == nil || stream.IsDefault && !defaultAudio.IsDefault {
				defaultAudio = stream
			}
		}
	}
	if selected == nil || selected.IsExternal {
		return result, errAudioRequestUnsupported
	}
	result.SourceContainer, result.StartTicks = audioSourceContainer(source), options.request.StartTimeTicks
	result.AudioStreamIndex = selected.Index
	selectedIndex := selected.Index
	options.request.AudioStreamIndex = &selectedIndex
	if options.static != nil && *options.static {
		if len(containers) != 0 && !audioContainerMatches(containers, result.SourceContainer, *defaultAudio) {
			return result, errAudioRequestUnsupported
		}
		result.Original, result.AudioStreamIndex = true, defaultAudio.Index
		return result, nil
	}
	if err := audioUnsupportedTransforms(values); err != nil {
		return result, err
	}
	matching := audioContainerMatches(containers, result.SourceContainer, *selected)
	explicit := audioExplicitConversion(values, options)
	originalCandidate := matching
	if len(containers) == 0 {
		// Universal omission declares no container restriction. Conversion
		// targets remain fallbacks, just as with a matching explicit capability.
		originalCandidate = universal || !explicit
	}
	if options.static != nil && !*options.static || selected.Index != defaultAudio.Index || !universal && explicit {
		originalCandidate = false
	}
	if originalCandidate && audioOriginalFits(source, *selected, options.request) {
		result.Original = true
		return result, nil
	}
	// The transcoding ceiling applies only after original-file selection. It
	// does not reject compatible originals or replace an exact output target.
	if ceiling := options.transcodingMaxAudioChannels; ceiling != nil &&
		(options.request.MaxAudioChannels == nil || *ceiling < *options.request.MaxAudioChannels) {
		options.request.MaxAudioChannels = ceiling
	}
	if source.Info.DurationTicks <= 0 || result.StartTicks >= source.Info.DurationTicks {
		return result, errAudioRequestUnsupported
	}
	protocol := strings.ToLower(values["transcodingprotocol"])
	if protocol == "" {
		protocol = "http"
	}
	if protocol != "http" && protocol != "progressive" && protocol != "hls" {
		return result, errAudioRequestUnsupported
	}
	output := ""
	if raw := values["transcodingcontainer"]; raw != "" {
		output, err = audioSelector(raw)
		if err != nil {
			return result, err
		}
	}
	if !universal && len(containers) > 0 {
		if output != "" && audioCapability(output) != audioCapability(containers[0]) {
			return result, errAudioRequestInvalid
		}
		output = containers[0]
	}
	if protocol == "hls" {
		if output == "" {
			output = "ts"
		}
		if output != "ts" && output != "mpegts" && output != "mpeg-ts" {
			return result, errAudioRequestUnsupported
		}
		if segment := strings.ToLower(values["segmentcontainer"]); segment != "" && segment != "ts" && segment != "mpegts" && segment != "mpeg-ts" {
			return result, errAudioRequestUnsupported
		}
		if len(codecs) == 0 {
			codecs = []string{"aac"}
		}
		decision, err := audioHLSDecision(source, options.request, values, codecs, limits)
		result.HLS = &decision
		return result, err
	}
	if output == "" {
		output = "mp3"
	}
	if values["segmentcontainer"] != "" || values["segmentlength"] != "" {
		return result, errAudioRequestUnsupported
	}
	container, defaultCodec, err := audioProgressiveFormat(output)
	if err != nil {
		return result, err
	}
	if len(codecs) == 0 {
		codecs = []string{defaultCodec}
	}
	for _, codec := range codecs {
		if codec != "copy" && !transcode.ProgressiveContainerSupportsCodec(container, codec) {
			continue
		}
		request := options.request
		request.OutputContainer, request.AudioCodec = container, codec
		decision, err := playback.PlanProgressiveAudio(source, request, limits)
		result.Progressive = &decision
		if err != nil {
			return result, errAudioRequestInvalid
		}
		if decision.Plan != nil {
			return result, nil
		}
	}
	return result, errAudioRequestUnsupported
}

func audioHLSDecision(source playback.Source, options playback.ProgressiveAudioRequest, values map[string]string, codecs []string, limits playback.ConversionLimits) (playback.ConversionDecision, error) {
	for _, field := range []struct {
		name    string
		maximum int64
	}{{"minsegments", 1}, {"maxmanifestsubtitles", 0}} {
		value, err := hlsQueryInteger(values, 0, math.MaxInt32, field.name)
		if err != nil {
			return playback.ConversionDecision{}, errAudioRequestInvalid
		}
		if value != nil && *value > field.maximum {
			return playback.ConversionDecision{}, errAudioRequestUnsupported
		}
	}
	if values["manifestsubtitles"] != "" || values["transcodeseekinfo"] != "" && !strings.EqualFold(values["transcodeseekinfo"], "auto") {
		return playback.ConversionDecision{}, errAudioRequestUnsupported
	}
	var chosen []string
	copyOnly := false
	for _, codec := range codecs {
		if codec == "aac" || codec == "mp3" {
			chosen = append(chosen, codec)
		} else if codec == "copy" && len(codecs) == 1 {
			copyOnly = true
			for _, stream := range source.Info.Streams {
				if stream.Index == *options.AudioStreamIndex && (stream.Codec == "aac" || stream.Codec == "mp3") {
					chosen = append(chosen, stream.Codec)
				}
			}
		}
	}
	if len(chosen) == 0 {
		return playback.ConversionDecision{}, errAudioRequestUnsupported
	}
	profile := playback.TranscodingProfile{Type: playback.DlnaProfileTypeAudio, Container: "ts",
		Protocol: "hls", Context: playback.EncodingContextStreaming, AudioCodec: strings.Join(chosen, ",")}
	length, err := hlsQueryInteger(values, 1, 10, "segmentlength")
	if err != nil {
		return playback.ConversionDecision{}, errAudioRequestInvalid
	}
	if length != nil {
		seconds := int(*length)
		profile.SegmentLength = &seconds
	}
	var conditions []playback.ProfileCondition
	for _, field := range []struct {
		value    *int
		property playback.ProfileConditionValue
		exact    bool
	}{
		{options.AudioChannels, playback.ProfileConditionValueAudioChannels, true},
		{options.MaxAudioChannels, playback.ProfileConditionValueAudioChannels, false},
		{options.AudioSampleRate, playback.ProfileConditionValueAudioSampleRate, true},
		{options.MaxSampleRate, playback.ProfileConditionValueAudioSampleRate, false},
		{options.AudioBitDepth, playback.ProfileConditionValueAudioBitDepth, true},
	} {
		if field.value != nil {
			operator := playback.ProfileConditionTypeLessThanEqual
			if field.exact {
				operator = playback.ProfileConditionTypeEquals
			}
			conditions = append(conditions, hlsRequiredCondition(field.property, operator, strconv.Itoa(*field.value)))
		}
	}
	if options.AudioBitrate != nil {
		conditions = append(conditions, hlsRequiredCondition(playback.ProfileConditionValueAudioBitrate,
			playback.ProfileConditionTypeEquals, strconv.FormatInt(*options.AudioBitrate, 10)))
	}
	request := playback.Request{
		AudioStreamIndex: options.AudioStreamIndex, StartTimeTicks: &options.StartTimeTicks,
		MaxStreamingBitrate: options.MaxBitrate, MaxAudioChannels: options.MaxAudioChannels, AllowAudioStreamCopy: options.AllowAudioStreamCopy,
		DeviceProfile: &playback.DeviceProfile{
			TranscodingProfiles: []playback.TranscodingProfile{profile},
			CodecProfiles:       []playback.CodecProfile{{Type: playback.CodecTypeAudio, Container: "ts", Codec: strings.Join(chosen, ","), Conditions: conditions}},
		},
	}
	decision, err := playback.PlanConversion(source, request, limits)
	if err != nil {
		return decision, errAudioRequestInvalid
	}
	if decision.Plan == nil || copyOnly && decision.Plan.AudioCodec != "copy" {
		decision.Plan = nil
		return decision, errAudioRequestUnsupported
	}
	return decision, nil
}
