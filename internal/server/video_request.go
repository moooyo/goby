package server

import (
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/playback"
)

var (
	errVideoRequestInvalid     = errors.New("invalid video request")
	errVideoRequestUnsupported = errors.New("unsupported video request")
)

type videoRequestResult struct {
	Original   bool
	Conversion playback.ConversionDecision
	StartTicks int64
}

func videoContainer(value string) string {
	switch strings.ToLower(value) {
	case "mp4", "m4v", "mov", "fmp4":
		return "mp4"
	case "mpegts", "mpeg-ts", "ts":
		return "ts"
	case "matroska", "mkv":
		return "mkv"
	default:
		return strings.ToLower(value)
	}
}

func videoQueryFrameRate(values map[string]string, name string) (*float64, error) {
	raw, exists := values[name]
	if !exists {
		return nil, nil
	}
	if len(raw) > 32 {
		return nil, errVideoRequestInvalid
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 1 || value > 240 {
		return nil, errVideoRequestInvalid
	}
	return &value, nil
}

func videoUnsupportedTransforms(source playback.Source, values map[string]string) error {
	if values["livestreamid"] != "" {
		return errVideoRequestUnsupported
	}
	for _, name := range []string{"videorange", "videorangetype"} {
		if value := strings.ToLower(values[name]); value != "" && value != "sdr" {
			return errVideoRequestUnsupported
		}
	}
	for _, name := range []string{"copytimestamps", "breakonnonkeyframes", "enablempegtsm2tsmode", "burnsubtitles", "burninsubtitles", "deinterlace", "deinterlacevideo", "enabletonemapping", "tonemapping", "enablehdr", "hdr"} {
		value, err := hlsQueryBoolean(values, name)
		if err != nil {
			return errVideoRequestInvalid
		}
		if value != nil && *value {
			return errVideoRequestUnsupported
		}
	}
	for _, name := range []string{"audiofilter", "videofilter", "filtercomplex", "subtitlecodec", "manifestsubtitles", "segmentcontainer", "segmentlength", "minsegments", "audiobitdepth", "channelmap", "channelmapping", "tonemappingalgorithm", "videoprofile", "videolevel", "pixelformat", "colortransfer", "colorspace", "colorprimaries"} {
		if values[name] != "" {
			return errVideoRequestUnsupported
		}
	}
	for _, name := range []string{"protocol", "transcodingprotocol"} {
		if value := strings.ToLower(values[name]); value != "" && value != "http" && value != "progressive" {
			return errVideoRequestUnsupported
		}
	}
	if value := strings.ToLower(values["transcodeseekinfo"]); value != "" && value != "auto" {
		return errVideoRequestUnsupported
	}
	for _, name := range []string{"subtitlemethod", "subtitledeliverymethod"} {
		if value := strings.ToLower(values[name]); value != "" && value != "none" && value != "external" {
			return errVideoRequestUnsupported
		}
	}
	if raw, exists := values["subtitlestreamindex"]; exists {
		index, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || index < -1 {
			return errVideoRequestInvalid
		}
		if index >= 0 {
			found := false
			for _, stream := range source.Info.Streams {
				found = found || stream.Index == int(index) && stream.CodecType == "subtitle" && stream.IsExternal && stream.IsTextSubtitleStream
			}
			if !found {
				return errVideoRequestUnsupported
			}
		}
	}
	return nil
}

// videoRequestDecision maps declared output settings to a concrete MP4 plan.
// Legacy unconstrained requests and Static=true retain original-file delivery;
// original positions are client-side seeks rather than guessed byte offsets.
func videoRequestDecision(source playback.Source, values map[string]string, suffix string, limits playback.ConversionLimits) (videoRequestResult, error) {
	var result videoRequestResult
	values, err := hlsQueryValues(values)
	if err != nil {
		return result, errVideoRequestInvalid
	}
	options, err := audioParseOptions(values)
	if err != nil {
		return result, errVideoRequestInvalid
	}
	result.StartTicks = options.request.StartTimeTicks
	legacyPosition := false
	if raw, exists := values["startpositionticks"]; exists {
		position, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || position < 0 {
			return result, errVideoRequestInvalid
		}
		legacyPosition = true
	}
	container := videoContainer(suffix)
	if raw := values["container"]; raw != "" {
		selector, err := audioSelector(raw)
		if err != nil {
			return result, errVideoRequestInvalid
		}
		selector = videoContainer(selector)
		if container != "" && container != selector {
			return result, errVideoRequestInvalid
		}
		container = selector
	}
	originalContainer := videoContainer(media.CanonicalContainer(source.Info, source.Path))
	explicit := options.static != nil && !*options.static
	for _, name := range []string{
		"videocodec", "audiocodec", "videobitrate", "audiobitrate", "maxstreamingbitrate", "maxvideobitrate", "maxaudiobitrate",
		"width", "height", "maxwidth", "maxheight", "framerate", "maxframerate", "audiochannels", "maxaudiochannels", "audiobitdepth",
		"transcodingmaxaudiochannels", "audiosamplerate", "maxsamplerate", "videostreamindex", "audiostreamindex", "allowvideostreamcopy",
		"allowaudiostreamcopy", "allowinterlacedvideostreamcopy", "enableautostreamcopy", "protocol", "transcodingprotocol", "transcodingcontainer",
		"copytimestamps", "breakonnonkeyframes", "enablempegtsm2tsmode", "burnsubtitles", "burninsubtitles", "deinterlace", "deinterlacevideo",
		"enabletonemapping", "tonemapping", "enablehdr", "hdr", "audiofilter", "videofilter", "filtercomplex", "subtitlecodec", "manifestsubtitles",
		"segmentcontainer", "segmentlength", "minsegments", "channelmap", "channelmapping", "tonemappingalgorithm", "videoprofile", "videolevel",
		"pixelformat", "colortransfer", "colorspace", "colorprimaries", "transcodeseekinfo",
		"subtitlemethod", "subtitledeliverymethod", "subtitlestreamindex", "livestreamid", "videorange", "videorangetype",
	} {
		_, supplied := values[name]
		explicit = explicit || supplied
	}
	if options.static != nil && *options.static || !explicit && (container == "" || container == originalContainer) {
		if container != "" && container != originalContainer {
			return result, errVideoRequestUnsupported
		}
		result.Original = true
		return result, nil
	}
	if legacyPosition {
		return result, errVideoRequestUnsupported
	}
	if err := videoUnsupportedTransforms(source, values); err != nil {
		return result, err
	}
	if output := values["transcodingcontainer"]; output != "" {
		output = videoContainer(output)
		if container != "" && container != output {
			return result, errVideoRequestInvalid
		}
		container = output
	}
	if container == "" {
		container = "mp4"
	}
	if container != "mp4" {
		return result, errVideoRequestUnsupported
	}
	request := playback.ProgressiveVideoRequest{OutputContainer: container, StartTimeTicks: result.StartTicks,
		AudioStreamIndex: options.request.AudioStreamIndex, AudioBitrate: options.request.AudioBitrate,
		AudioChannels: options.request.AudioChannels, AudioSampleRate: options.request.AudioSampleRate,
		MaxAudioChannels: options.request.MaxAudioChannels, MaxSampleRate: options.request.MaxSampleRate,
		MaxBitrate: options.request.MaxBitrate, AllowAudioStreamCopy: options.request.AllowAudioStreamCopy}
	if ceiling := options.transcodingMaxAudioChannels; ceiling != nil && (request.MaxAudioChannels == nil || *ceiling < *request.MaxAudioChannels) {
		request.MaxAudioChannels = ceiling
	}
	for _, field := range []struct {
		name string
		min  int64
		max  int64
		to   **int
	}{{"videostreamindex", 0, math.MaxInt32, &request.VideoStreamIndex}, {"width", 1, 8192, &request.Width},
		{"height", 1, 8192, &request.Height}, {"maxwidth", 1, 8192, &request.MaxWidth}, {"maxheight", 1, 8192, &request.MaxHeight}} {
		value, err := hlsQueryInteger(values, field.min, field.max, field.name)
		if err != nil {
			return result, errVideoRequestInvalid
		}
		if value != nil {
			converted := int(*value)
			*field.to = &converted
		}
	}
	for _, field := range []struct {
		name string
		to   **int64
	}{{"videobitrate", &request.VideoBitrate}, {"maxvideobitrate", &request.MaxVideoBitrate}, {"maxaudiobitrate", &request.MaxAudioBitrate}} {
		value, err := hlsQueryInteger(values, 1, 1_000_000_000, field.name)
		if err != nil {
			return result, errVideoRequestInvalid
		}
		*field.to = value
	}
	for _, field := range []struct {
		name string
		to   **bool
	}{{"allowvideostreamcopy", &request.AllowVideoStreamCopy}, {"allowinterlacedvideostreamcopy", &request.AllowInterlacedVideoStreamCopy}} {
		value, err := hlsQueryBoolean(values, field.name)
		if err != nil {
			return result, errVideoRequestInvalid
		}
		*field.to = value
	}
	if options.autoCopy != nil && !*options.autoCopy {
		disabled := false
		request.AllowVideoStreamCopy, request.AllowAudioStreamCopy = &disabled, &disabled
	}
	request.FrameRate, err = videoQueryFrameRate(values, "framerate")
	if err != nil {
		return result, err
	}
	request.MaxFrameRate, err = videoQueryFrameRate(values, "maxframerate")
	if err != nil {
		return result, err
	}
	videoCodecs, audioCodecs := []string{"h264"}, []string{"aac"}
	if raw := values["videocodec"]; raw != "" {
		videoCodecs, err = audioSelectors(raw)
		if err != nil || len(videoCodecs) > 8 {
			return result, errVideoRequestInvalid
		}
	}
	if raw := values["audiocodec"]; raw != "" {
		audioCodecs, err = audioSelectors(raw)
		if err != nil || len(audioCodecs) > 8 {
			return result, errVideoRequestInvalid
		}
	}
	hasAudio := false
	for _, stream := range source.Info.Streams {
		hasAudio = hasAudio || stream.CodecType == "audio" && !stream.IsExternal
	}
	if !hasAudio && values["audiocodec"] == "" {
		audioCodecs = []string{""}
	}
	for _, video := range videoCodecs {
		if video != "h264" && video != "copy" {
			continue
		}
		for _, audio := range audioCodecs {
			if audio != "" && audio != "aac" && audio != "copy" && audio != "none" {
				continue
			}
			request.VideoCodec, request.AudioCodec = video, audio
			decision, err := playback.PlanProgressiveVideo(source, request, limits)
			if err != nil {
				return result, errVideoRequestInvalid
			}
			if decision.Plan != nil {
				result.Conversion = playback.ConversionDecision{Plan: decision.Plan, OutputSource: decision.OutputSource, Method: decision.Method, Reasons: decision.Reasons}
				return result, nil
			}
		}
	}
	return result, errVideoRequestUnsupported
}
