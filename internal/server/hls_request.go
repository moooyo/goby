package server

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/moooyo/goby/internal/config"
	"github.com/moooyo/goby/internal/identity"
	"github.com/moooyo/goby/internal/playback"
)

var (
	errHLSRequestInvalid     = errors.New("invalid HLS conversion request")
	errHLSRequestUnsupported = errors.New("unsupported HLS conversion request")
)

const (
	maxHLSQueryFields = 256
	maxHLSQueryText   = 4096
	maxHLSCodecNames  = 8
)

// hlsUserLimits combines the configured execution limits with the current
// persisted user policy. Administrators obey the same explicit playback flags.
func hlsUserLimits(cfg config.TranscodingConfig, user identity.User) playback.ConversionLimits {
	limits := playback.ConversionLimits{
		MaxBitrate: cfg.MaxBitrate, MaxWidth: cfg.MaxWidth, MaxHeight: cfg.MaxHeight,
		MaxAudioChannels: cfg.MaxAudioChannels, Hardware: cfg.Hardware,
	}
	if !cfg.Enabled || user.IsDisabled || !utf8.Valid(user.Policy) {
		return limits
	}
	var policy map[string]json.RawMessage
	if json.Unmarshal(user.Policy, &policy) != nil || policy == nil {
		return limits
	}
	enabled := func(name string) bool {
		raw, exists := policy[name]
		if !exists {
			return true
		}
		var value *bool
		return json.Unmarshal(raw, &value) == nil && value != nil && *value
	}
	if !enabled("EnableMediaPlayback") {
		return limits
	}
	limits.AllowRemux = enabled("EnablePlaybackRemuxing")
	limits.AllowAudioTranscode = enabled("EnableAudioPlaybackTranscoding")
	limits.AllowVideoTranscode = enabled("EnableVideoPlaybackTranscoding")
	return limits
}

func hlsQueryValues(values map[string]string) (map[string]string, error) {
	if len(values) > maxHLSQueryFields {
		return nil, errHLSRequestInvalid
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		if len(key) == 0 || len(key) > 128 || len(value) > maxHLSQueryText ||
			!utf8.ValidString(key) || !utf8.ValidString(value) ||
			strings.IndexFunc(key, unicode.IsControl) >= 0 || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return nil, errHLSRequestInvalid
		}
		key = strings.ToLower(key)
		if previous, exists := result[key]; exists && previous != value {
			return nil, errHLSRequestInvalid
		}
		result[key] = value
	}
	return result, nil
}

func hlsQueryInteger(values map[string]string, minimum, maximum int64, names ...string) (*int64, error) {
	var result *int64
	for _, name := range names {
		raw, exists := values[name]
		if !exists {
			continue
		}
		if len(raw) > 32 {
			return nil, errHLSRequestInvalid
		}
		value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil || value < minimum || value > maximum || result != nil && *result != value {
			return nil, errHLSRequestInvalid
		}
		result = &value
	}
	return result, nil
}

func hlsQueryBoolean(values map[string]string, name string) (*bool, error) {
	raw, exists := values[name]
	if !exists {
		return nil, nil
	}
	if len(raw) > 5 {
		return nil, errHLSRequestInvalid
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, errHLSRequestInvalid
	}
	return &value, nil
}

func hlsQueryFramerate(values map[string]string) (*float64, error) {
	var result *float64
	for _, name := range []string{"framerate", "maxframerate"} {
		raw, exists := values[name]
		if !exists {
			continue
		}
		if len(raw) > 32 {
			return nil, errHLSRequestInvalid
		}
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 1 || value > 240 || result != nil && *result != value {
			return nil, errHLSRequestInvalid
		}
		result = &value
	}
	return result, nil
}

func hlsQueryCodecs(values map[string]string, name, defaultCodec string, supported ...string) (string, error) {
	raw, exists := values[name]
	if !exists {
		return defaultCodec, nil
	}
	if len(raw) > 256 {
		return "", errHLSRequestInvalid
	}
	names := strings.Split(raw, ",")
	if len(names) > maxHLSCodecNames {
		return "", errHLSRequestInvalid
	}
	var selected []string
	seen := map[string]bool{}
	for _, codec := range names {
		codec = strings.ToLower(strings.TrimSpace(codec))
		if len(codec) == 0 || len(codec) > 32 {
			return "", errHLSRequestInvalid
		}
		for _, character := range codec {
			if !(character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '_' || character == '-') {
				return "", errHLSRequestInvalid
			}
		}
		for _, candidate := range supported {
			if codec == candidate && !seen[codec] {
				selected = append(selected, codec)
				seen[codec] = true
			}
		}
	}
	if len(selected) == 0 {
		return "", errHLSRequestUnsupported
	}
	return strings.Join(selected, ","), nil
}

func hlsUnsupportedOptions(values map[string]string) error {
	if protocol := strings.ToLower(values["protocol"]); protocol != "" && protocol != "hls" {
		return errHLSRequestUnsupported
	}
	if seekInfo := strings.ToLower(values["transcodeseekinfo"]); seekInfo != "" && seekInfo != "auto" {
		return errHLSRequestUnsupported
	}
	for _, name := range []string{"container", "segmentcontainer"} {
		if raw, exists := values[name]; exists {
			switch strings.ToLower(strings.TrimSpace(raw)) {
			case "ts", "mpegts", "mpeg-ts":
			case "":
				return errHLSRequestInvalid
			default:
				return errHLSRequestUnsupported
			}
		}
	}
	for _, name := range []string{
		"copytimestamps", "breakonnonkeyframes", "enablempegtsm2tsmode",
		"burnsubtitles", "burninsubtitles", "deinterlace", "deinterlacevideo",
		"enabletonemapping", "tonemapping", "enablehdr", "hdr",
	} {
		value, err := hlsQueryBoolean(values, name)
		if err != nil {
			return err
		}
		if value != nil && *value {
			return errHLSRequestUnsupported
		}
	}
	for _, name := range []string{
		"subtitlecodec", "manifestsubtitles", "tonemappingalgorithm", "videoprofile",
		"videolevel", "pixelformat", "colortransfer", "colorspace", "colorprimaries",
		"videofilter", "audiofilter", "filtercomplex", "startpositionticks",
	} {
		if value := values[name]; value != "" {
			return errHLSRequestUnsupported
		}
	}
	for _, name := range []string{"subtitlemethod", "subtitledeliverymethod"} {
		if value := strings.ToLower(values[name]); value != "" && value != "none" && value != "external" {
			return errHLSRequestUnsupported
		}
	}
	for _, name := range []string{"videorange", "videorangetype"} {
		if value := strings.ToLower(values[name]); value != "" && value != "sdr" {
			return errHLSRequestUnsupported
		}
	}
	for _, option := range []struct {
		name    string
		maximum int64
	}{{"minsegments", 1}, {"maxmanifestsubtitles", 0}} {
		value, err := hlsQueryInteger(values, 0, 1<<31-1, option.name)
		if err != nil {
			return err
		}
		if value != nil && *value > option.maximum {
			return errHLSRequestUnsupported
		}
	}
	return nil
}

func hlsRequiredCondition(property playback.ProfileConditionValue, operator playback.ProfileConditionType, value string) playback.ProfileCondition {
	required := true
	return playback.ProfileCondition{Property: property, Condition: operator, Value: value, IsRequired: &required}
}

// hlsRequestConversion maps an explicit manual HLS query into an output
// capability request. H.264/AAC are Goby's defaults when codecs are omitted,
// not a claim that an unknown client accepts every original media format.
// The caller owns authentication, source access and the overall query byte cap.
func hlsRequestConversion(values map[string]string, source playback.Source, limits playback.ConversionLimits) (playback.ConversionDecision, error) {
	values, err := hlsQueryValues(values)
	if err != nil {
		return playback.ConversionDecision{}, err
	}
	if err := hlsUnsupportedOptions(values); err != nil {
		return playback.ConversionDecision{}, err
	}
	videoCodec, err := hlsQueryCodecs(values, "videocodec", "h264", "h264")
	if err != nil {
		return playback.ConversionDecision{}, err
	}
	audioCodec, err := hlsQueryCodecs(values, "audiocodec", "aac", "aac", "mp3")
	if err != nil {
		return playback.ConversionDecision{}, err
	}
	kind, audioKind := playback.DlnaProfileTypeVideo, playback.CodecTypeVideoAudio
	if source.ItemType == "Audio" {
		kind, audioKind = playback.DlnaProfileTypeAudio, playback.CodecTypeAudio
	}
	profile := playback.TranscodingProfile{
		Type: kind, Container: "ts", Protocol: "hls", Context: playback.EncodingContextStreaming,
		VideoCodec: videoCodec, AudioCodec: audioCodec,
	}
	request := playback.Request{ID: source.ItemID, MediaSourceID: source.MediaSourceID}
	if source.Info.DurationTicks <= 0 {
		return playback.ConversionDecision{}, errHLSRequestUnsupported
	}
	request.StartTimeTicks, err = hlsQueryInteger(values, 0, source.Info.DurationTicks-1, "starttimeticks")
	if err != nil {
		return playback.ConversionDecision{}, err
	}
	request.MaxStreamingBitrate, err = hlsQueryInteger(values, 1, 1_000_000_000, "maxstreamingbitrate")
	if err != nil {
		return playback.ConversionDecision{}, err
	}
	var videoConditions, audioConditions []playback.ProfileCondition
	for _, field := range []struct {
		names    []string
		min, max int64
		property playback.ProfileConditionValue
		audio    bool
		exact    bool
	}{
		{[]string{"videobitrate"}, 1, 1_000_000_000, playback.ProfileConditionValueVideoBitrate, false, false},
		{[]string{"audiobitrate"}, 1, 1_000_000_000, playback.ProfileConditionValueAudioBitrate, true, false},
		{[]string{"maxwidth", "width"}, 1, 8192, playback.ProfileConditionValueWidth, false, false},
		{[]string{"maxheight", "height"}, 1, 8192, playback.ProfileConditionValueHeight, false, false},
		{[]string{"maxaudiochannels", "transcodingmaxaudiochannels", "audiochannels"}, 1, 8, playback.ProfileConditionValueAudioChannels, true, false},
		{[]string{"audiosamplerate"}, 1, 384_000, playback.ProfileConditionValueAudioSampleRate, true, true},
	} {
		value, err := hlsQueryInteger(values, field.min, field.max, field.names...)
		if err != nil {
			return playback.ConversionDecision{}, err
		}
		if value == nil {
			continue
		}
		operator := playback.ProfileConditionTypeLessThanEqual
		if field.exact {
			operator = playback.ProfileConditionTypeEquals
		}
		condition := hlsRequiredCondition(field.property, operator, strconv.FormatInt(*value, 10))
		if field.audio {
			audioConditions = append(audioConditions, condition)
		} else {
			videoConditions = append(videoConditions, condition)
		}
	}
	framerate, err := hlsQueryFramerate(values)
	if err != nil {
		return playback.ConversionDecision{}, err
	}
	if framerate != nil {
		videoConditions = append(videoConditions, hlsRequiredCondition(playback.ProfileConditionValueVideoFramerate,
			playback.ProfileConditionTypeLessThanEqual, strconv.FormatFloat(*framerate, 'g', -1, 64)))
	}
	if strings.EqualFold(values["videorange"], "SDR") || strings.EqualFold(values["videorangetype"], "SDR") {
		videoConditions = append(videoConditions, hlsRequiredCondition(playback.ProfileConditionValueVideoRange, playback.ProfileConditionTypeEquals, "SDR"))
	}
	for _, field := range []struct {
		name     string
		minimum  int64
		target   **int
		segment  bool
		subtitle bool
	}{
		{"audiostreamindex", 0, &request.AudioStreamIndex, false, false},
		{"subtitlestreamindex", -1, &request.SubtitleStreamIndex, false, true},
		{"segmentlength", 1, &profile.SegmentLength, true, false},
	} {
		maximum := int64(1<<31 - 1)
		if field.segment {
			maximum = 10
		}
		value, err := hlsQueryInteger(values, field.minimum, maximum, field.name)
		if err != nil {
			return playback.ConversionDecision{}, err
		}
		if value != nil {
			if field.subtitle && *value != -1 {
				return playback.ConversionDecision{}, errHLSRequestUnsupported
			}
			converted := int(*value)
			*field.target = &converted
		}
	}
	for _, field := range []struct {
		name   string
		target **bool
	}{
		{"allowvideostreamcopy", &request.AllowVideoStreamCopy},
		{"allowaudiostreamcopy", &request.AllowAudioStreamCopy},
		{"allowinterlacedvideostreamcopy", &request.AllowInterlacedVideoStreamCopy},
		{"enabledirectstream", &request.EnableDirectStream},
		{"enabletranscoding", &request.EnableTranscoding},
	} {
		value, err := hlsQueryBoolean(values, field.name)
		if err != nil {
			return playback.ConversionDecision{}, err
		}
		*field.target = value
	}
	autoCopy, err := hlsQueryBoolean(values, "enableautostreamcopy")
	if err != nil {
		return playback.ConversionDecision{}, err
	}
	if autoCopy != nil && !*autoCopy {
		disabledVideo, disabledAudio := false, false
		request.AllowVideoStreamCopy, request.AllowAudioStreamCopy = &disabledVideo, &disabledAudio
	}
	request.DeviceProfile = &playback.DeviceProfile{
		Name: "Goby explicit HLS request", SupportedMediaTypes: string(kind),
		TranscodingProfiles: []playback.TranscodingProfile{profile},
		CodecProfiles: []playback.CodecProfile{
			{Type: playback.CodecTypeVideo, Container: "ts", Codec: videoCodec, Conditions: videoConditions},
			{Type: audioKind, Container: "ts", Codec: audioCodec, Conditions: audioConditions},
		},
	}
	decision, err := playback.PlanConversion(source, request, limits)
	if err != nil {
		return decision, errHLSRequestInvalid
	}
	if decision.Plan == nil {
		return decision, errHLSRequestUnsupported
	}
	return decision, nil
}
