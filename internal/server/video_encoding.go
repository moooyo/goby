package server

import (
	"net/url"
	"strconv"
	"strings"

	"github.com/moooyo/goby/internal/media"
	"github.com/moooyo/goby/internal/transcode"
)

// videoQueryEncoding accepts only output settings represented by the closed
// encoder matrix. Codec/profile compatibility is decided by the planner.
func videoQueryEncoding(values map[string]string) (*int, string, error) {
	var depth *int
	if raw, supplied := values["videobitdepth"]; supplied {
		value, err := strconv.Atoi(raw)
		if err != nil || value != 8 && value != 10 || raw != strconv.Itoa(value) {
			return nil, "", errHLSRequestInvalid
		}
		depth = &value
	}
	profile := ""
	if raw, supplied := values["videoprofile"]; supplied {
		if raw == "" || len(raw) > 32 {
			return nil, "", errHLSRequestInvalid
		}
		for _, character := range raw {
			if !(character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' || character >= '0' && character <= '9' || character == ' ') {
				return nil, "", errHLSRequestInvalid
			}
		}
		profile = videoEncodingProfile(raw)
		if profile == "" {
			return nil, "", errHLSRequestUnsupported
		}
	}
	return depth, profile, nil
}

func videoEncodingProfile(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "baseline":
		return "baseline"
	case "main":
		return "main"
	case "high":
		return "high"
	case "main10", "main 10":
		return "main10"
	default:
		return ""
	}
}

func videoProfileCondition(profile string) string {
	if profile == "main10" {
		return "Main 10"
	}
	return profile
}

func videoQueryRange(values map[string]string) (string, error) {
	result := ""
	for _, name := range []string{"videorange", "videorangetype"} {
		if raw, supplied := values[name]; supplied {
			value := strings.ToUpper(strings.TrimSpace(raw))
			if value == "" {
				return "", errHLSRequestInvalid
			}
			if value != "SDR" && value != "HDR10" {
				return "", errHLSRequestUnsupported
			}
			if result != "" && result != value {
				return "", errHLSRequestInvalid
			}
			result = value
		}
	}
	return result, nil
}

// setVideoEncodingQuery publishes only encoder settings represented in Plan.
// Copied streams have no encoder settings and retain their source facts.
func setVideoEncodingQuery(values url.Values, plan transcode.Plan) {
	if plan.VideoCodec == "" || plan.VideoCodec == "copy" {
		return
	}
	values.Set("VideoCodec", plan.VideoCodec)
	values.Set("VideoBitDepth", strconv.Itoa(transcode.VideoOutputBitDepth(plan)))
	if profile := transcode.VideoOutputProfile(plan); profile != "" {
		values.Set("VideoProfile", profile)
	}
	if plan.VideoFilters.OutputRange != "" {
		values.Set("VideoRange", strings.ToUpper(plan.VideoFilters.OutputRange))
	}
}

func hlsVideoEncodingQuery(session *hlsSession) url.Values {
	values := url.Values{}
	plan := session.key.plan
	if plan.VideoCodec != "copy" {
		setVideoEncodingQuery(values, plan)
		return values
	}
	for _, stream := range session.output.Info.Streams {
		if stream.CodecType != "video" || stream.IsAttachedPicture {
			continue
		}
		values.Set("VideoCodec", strings.ToLower(stream.Codec))
		if depth := media.EffectiveVideoBitDepth(stream); (depth == 8 || depth == 10) && !media.VideoBitDepthConflict(stream) {
			values.Set("VideoBitDepth", strconv.Itoa(depth))
		}
		if profile := videoEncodingProfile(stream.Profile); profile != "" {
			values.Set("VideoProfile", profile)
		}
		if stream.VideoRangeKnown && (stream.VideoRange == "SDR" || stream.VideoRange == "HDR10") {
			values.Set("VideoRange", stream.VideoRange)
		}
		break
	}
	return values
}

// A registered HLS revision can echo its output facts, but query edits cannot
// change the codec or precision of media produced for that revision.
func hlsVideoEncodingMatches(session *hlsSession, values map[string]string) bool {
	expected := hlsVideoEncodingQuery(session)
	for _, name := range []string{"VideoCodec", "VideoBitDepth", "VideoProfile", "VideoRange"} {
		if value, supplied := values[strings.ToLower(name)]; supplied {
			if expected.Get(name) == "" || value != expected.Get(name) {
				return false
			}
		}
	}
	if value, supplied := values["videorangetype"]; supplied && (expected.Get("VideoRange") == "" || value != expected.Get("VideoRange")) {
		return false
	}
	return true
}
