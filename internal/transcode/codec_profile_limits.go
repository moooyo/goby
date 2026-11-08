package transcode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

var ErrInvalidCodecProfileLimits = errors.New("invalid codec profile limits")

// CodecProfileLimits bounds new encoded output profiles. These are explicit
// supported profile sets, independent of native enum ordinals or encoder IDs.
// The selected profile remains a concrete output fact in the admitted plan.
type CodecProfileLimits struct {
	H264 string `json:"H264"`
	HEVC string `json:"HEVC"`
}

// DefaultCodecProfileLimits leaves every implemented profile eligible.
func DefaultCodecProfileLimits() CodecProfileLimits {
	return CodecProfileLimits{H264: "high", HEVC: "main10"}
}

func ValidateCodecProfileLimits(value CodecProfileLimits) error {
	if value.H264 != "baseline" && value.H264 != "main" && value.H264 != "high" {
		return fmt.Errorf("%w: H.264 profile", ErrInvalidCodecProfileLimits)
	}
	if value.HEVC != "main" && value.HEVC != "main10" {
		return fmt.Errorf("%w: HEVC profile", ErrInvalidCodecProfileLimits)
	}
	return nil
}

// ResolveCodecProfileLimits preserves legacy planner callers that supplied no
// managed policy. Stored and administrator groups must instead be complete.
func ResolveCodecProfileLimits(value CodecProfileLimits) (CodecProfileLimits, error) {
	if value == (CodecProfileLimits{}) {
		return DefaultCodecProfileLimits(), nil
	}
	return value, ValidateCodecProfileLimits(value)
}

// UnmarshalJSON requires both supported codec limits exactly once. A profile
// limit cannot silently accept an unknown or partially supplied policy group.
func (value *CodecProfileLimits) UnmarshalJSON(data []byte) error {
	if value == nil || len(data) == 0 || len(data) > 512 {
		return ErrInvalidCodecProfileLimits
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return ErrInvalidCodecProfileLimits
	}
	var result CodecProfileLimits
	seen := make(map[string]bool, 2)
	for decoder.More() {
		token, err := decoder.Token()
		field, ok := token.(string)
		if err != nil || !ok || seen[field] || field != "H264" && field != "HEVC" {
			return ErrInvalidCodecProfileLimits
		}
		token, err = decoder.Token()
		profile, ok := token.(string)
		if err != nil || !ok {
			return ErrInvalidCodecProfileLimits
		}
		seen[field] = true
		switch field {
		case "H264":
			result.H264 = profile
		case "HEVC":
			result.HEVC = profile
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') || len(seen) != 2 {
		return ErrInvalidCodecProfileLimits
	}
	if _, err := decoder.Token(); err != io.EOF {
		return ErrInvalidCodecProfileLimits
	}
	if err := ValidateCodecProfileLimits(result); err != nil {
		return err
	}
	*value = result
	return nil
}

// VideoProfileAllowed tests an implemented profile against its explicit limit.
// An upper bound admits lower profiles without forcing one output profile.
func VideoProfileAllowed(value CodecProfileLimits, codec, profile string) bool {
	resolved, err := ResolveCodecProfileLimits(value)
	if err != nil {
		return false
	}
	switch codec {
	case "h264":
		switch resolved.H264 {
		case "baseline":
			return profile == "baseline"
		case "main":
			return profile == "baseline" || profile == "main"
		case "high":
			return profile == "baseline" || profile == "main" || profile == "high"
		}
	case "hevc":
		switch resolved.HEVC {
		case "main":
			return profile == "main"
		case "main10":
			return profile == "main" || profile == "main10"
		}
	default:
		return true
	}
	return false
}

// ConfiguredVideoProfileAllowed gates the final encoder choice at admission.
// Copy and non-video plans never reinterpret source profiles as encoding policy.
func ConfiguredVideoProfileAllowed(value CodecProfileLimits, plan Plan) bool {
	if !VideoEncodingSupported(plan.VideoCodec) {
		return true
	}
	return VideoProfileAllowed(value, plan.VideoCodec, VideoOutputProfile(plan))
}
