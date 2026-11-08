package transcode

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestCodecProfileLimitsRejectAmbiguousAndUnsupportedGroups(t *testing.T) {
	for _, raw := range []string{
		`null`, `{}`, `{"H264":"main"}`, `{"H264":"high","HEVC":"main10","AV1":"main"}`,
		`{"H264":"baseline","H264":"high","HEVC":"main10"}`,
		`{"H264":null,"HEVC":"main10"}`, `{"H264":1,"HEVC":"main10"}`,
		`{"H264":"high10","HEVC":"main10"}`, `{"H264":"high","HEVC":"main12"}`,
		`{"H264":"high","HEVC":"main10"} {}`,
	} {
		t.Run(raw, func(t *testing.T) {
			value := DefaultCodecProfileLimits()
			if err := json.Unmarshal([]byte(raw), &value); err == nil {
				t.Fatalf("accepted an ambiguous or unsupported managed profile group: %v", err)
			}
			if value != DefaultCodecProfileLimits() {
				t.Fatal("invalid decoding replaced the caller's prior policy")
			}
		})
	}
	var decoded CodecProfileLimits
	if err := json.Unmarshal([]byte(`{"HEVC":"main","H264":"baseline"}`), &decoded); err != nil ||
		decoded != (CodecProfileLimits{H264: "baseline", HEVC: "main"}) {
		t.Fatalf("a complete unordered group did not preserve its supported limits: %+v, %v", decoded, err)
	}
	if _, err := ResolveCodecProfileLimits(CodecProfileLimits{}); err != nil {
		t.Fatal("an absent legacy planner policy did not retain the implemented profile set")
	}
	if err := ValidateCodecProfileLimits(CodecProfileLimits{}); !errors.Is(err, ErrInvalidCodecProfileLimits) {
		t.Fatal("a persisted policy was allowed to omit both profile limits")
	}
}

func TestCodecProfileLimitsLeaveCopiesAndUnmanagedFormatsEligible(t *testing.T) {
	limits := CodecProfileLimits{H264: "baseline", HEVC: "main"}
	for _, plan := range []Plan{
		{VideoCodec: "copy", VideoCopyCodec: "h264"},
		{VideoCodec: "copy", VideoCopyCodec: "hevc"},
		{VideoCodec: "av1", VideoProfile: "main", VideoBitDepth: 10},
		{AudioCodec: "aac"},
	} {
		if !ConfiguredVideoProfileAllowed(limits, plan) {
			t.Fatalf("an unrelated encoding limit removed an eligible copy or unmanaged output: %+v", plan)
		}
	}
	if ConfiguredVideoProfileAllowed(limits, Plan{VideoCodec: "h264"}) ||
		ConfiguredVideoProfileAllowed(limits, Plan{VideoCodec: "hevc", VideoBitDepth: 10}) {
		t.Fatal("a default-resolved encoded profile bypassed the configured upper bound")
	}
}
